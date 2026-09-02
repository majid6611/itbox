// Package radicalefs manages calendar-radicale's htpasswd-style users
// file the same way backend/internal/webdavfs manages hacdias/webdav's
// config.yml: Radicale has no live API for user management either, so
// this is the only source of truth, kept in sync by reading, editing,
// and writing it back through the shared Docker volume, then restarting
// the container to pick it up.
package radicalefs

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"it-platform/backend/internal/caldavclient"
)

type VolumeRW interface {
	ReadVolumeFile(ctx context.Context, volume, path string) (string, error)
	WriteVolumeFile(ctx context.Context, volume, path, content string) error
	RestartContainer(ctx context.Context, containerName string) error
}

// ServiceAccount and CompanyAccount are the two internal Radicale logins
// that share one generated secret (the module's service_password config
// field) — see manifest.yaml's own comment on why there are two rather
// than one.
const (
	ServiceAccount = "platform-service"
	CompanyAccount = "company"

	// CompanyCalendarPath is the one shared calendar every employee reads
	// and writes — see the rights file calendar-radicale's compose file
	// writes, which grants every authenticated user rw here specifically.
	CompanyCalendarPath = "/company/calendar/"
)

func personalCalendarPath(username string) string {
	return "/" + username + "/personal/"
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// parseUsers/formatUsers round-trip Radicale's htpasswd file: one
// "username:bcrypt-hash" line per account, order not meaningful.
func parseUsers(raw string) map[string]string {
	users := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		users[parts[0]] = parts[1]
	}
	return users
}

func formatUsers(users map[string]string) string {
	var b strings.Builder
	for username, hash := range users {
		fmt.Fprintf(&b, "%s:%s\n", username, hash)
	}
	return b.String()
}

func readUsers(ctx context.Context, docker VolumeRW, volume string) (map[string]string, error) {
	raw, err := docker.ReadVolumeFile(ctx, volume, "users")
	if err != nil {
		return nil, fmt.Errorf("read radicale users file: %w", err)
	}
	return parseUsers(raw), nil
}

func writeUsers(ctx context.Context, docker VolumeRW, volume, containerName string, users map[string]string) error {
	if err := docker.WriteVolumeFile(ctx, volume, "users", formatUsers(users)); err != nil {
		return fmt.Errorf("write radicale users file: %w", err)
	}
	if err := docker.RestartContainer(ctx, containerName); err != nil {
		return fmt.Errorf("restart radicale: %w", err)
	}
	return nil
}

// waitForLogin retries a plain authenticated GET against the server root
// until Radicale stops returning 401 — it needs a moment to come back up
// after each restart, so a bootstrap call right after writeUsers needs to
// tolerate that instead of racing it. Deliberately checks only that the
// *login* succeeds, not that any particular operation does: confirmed
// live that MKCALENDAR on a bare principal path ("/company/") gets a 403
// even under a rights rule that grants RW there, distinct from
// MKCALENDAR on a nested calendar collection ("/company/calendar/"),
// which is the operation this package actually relies on and verifies
// separately via EnsureCalendarAsOwner. Exercising the wrong operation
// here would misreport a real "not ready yet" as a permissions bug.
func waitForLogin(ctx context.Context, baseURL, username, password string) error {
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{}
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
		if err == nil {
			req.SetBasicAuth(username, password)
			resp, doErr := client.Do(req)
			if doErr == nil {
				resp.Body.Close()
				if resp.StatusCode != http.StatusUnauthorized {
					return nil
				}
				lastErr = fmt.Errorf("still unauthorized")
			} else {
				lastErr = doErr
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("radicale did not accept %s's login in time: %w", username, lastErr)
}

// Bootstrap runs once, right after calendar-radicale's container first
// comes up: seeds the two internal accounts (see ServiceAccount/
// CompanyAccount) and creates the shared company calendar. The company
// calendar's creation specifically has to happen authenticated as
// "company" itself — see caldavclient.EnsureCalendarAsOwner's doc
// comment — which is the whole reason CompanyAccount exists as a
// separate login from ServiceAccount rather than folding into it.
func Bootstrap(ctx context.Context, docker VolumeRW, volume, containerName, baseURL, servicePassword string) error {
	hash, err := hashPassword(servicePassword)
	if err != nil {
		return err
	}
	users, err := readUsers(ctx, docker, volume)
	if err != nil {
		return err
	}
	users[ServiceAccount] = hash
	users[CompanyAccount] = hash
	if err := writeUsers(ctx, docker, volume, containerName, users); err != nil {
		return err
	}

	if err := waitForLogin(ctx, baseURL, CompanyAccount, servicePassword); err != nil {
		return err
	}
	return caldavclient.EnsureCalendarAsOwner(ctx, baseURL, CompanyAccount, servicePassword, CompanyCalendarPath, "Company")
}

// SyncUser adds or updates one employee's Radicale login to match their
// current company password (the same plaintext the caller already has in
// hand at account-creation/reset time — this platform's own WebDAV
// module reuses it the same way, for the same reason: we set both
// systems' passwords ourselves, so there's no separate secret to
// generate or show anyone), then ensures their personal calendar exists.
func SyncUser(ctx context.Context, docker VolumeRW, volume, containerName, baseURL, username, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	users, err := readUsers(ctx, docker, volume)
	if err != nil {
		return err
	}
	users[username] = hash
	if err := writeUsers(ctx, docker, volume, containerName, users); err != nil {
		return err
	}

	if err := waitForLogin(ctx, baseURL, username, password); err != nil {
		return err
	}
	return caldavclient.EnsureCalendarAsOwner(ctx, baseURL, username, password, personalCalendarPath(username), "Personal")
}

// RemoveUser removes an employee's Radicale login, if present, and
// restarts the container. Their calendar data is left alone — revoking
// access isn't the same as deleting their events, matching RemoveUser's
// exact behavior in webdavfs.
func RemoveUser(ctx context.Context, docker VolumeRW, volume, containerName, username string) error {
	users, err := readUsers(ctx, docker, volume)
	if err != nil {
		return err
	}
	if _, ok := users[username]; !ok {
		return nil
	}
	delete(users, username)
	return writeUsers(ctx, docker, volume, containerName, users)
}

// PersonalCalendarPath is exported for the API layer to build event
// paths without duplicating the "/<user>/personal/" convention.
func PersonalCalendarPath(username string) string { return personalCalendarPath(username) }
