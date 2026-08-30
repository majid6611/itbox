package api

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/directory"
	"it-platform/backend/internal/webdavfs"
)

// directoryClient returns an LDAP client if the ldap-openldap module is
// installed and running, or available=false if there's no directory to
// talk to yet.
func (s *Server) directoryClient(ctx context.Context) (client *directory.Client, available bool, err error) {
	status, ok, err := s.Modules.GetInstalled(ctx, "ldap-openldap")
	if err != nil {
		return nil, false, err
	}
	if !ok || status.Status != "running" {
		return nil, false, nil
	}
	addr := s.Modules.ServiceAddr("ldap-openldap", "openldap", 389)
	baseDN := status.Config["base_dn"]
	bindDN := fmt.Sprintf("uid=admin,%s", baseDN)
	return directory.NewClient(addr, bindDN, status.Config["admin_password"], baseDN), true, nil
}

// ensureWebdavFolders creates one or more top-level folders in the WebDAV
// module (e.g. "shared" + a new user's name, or a new group's name), if
// that module is installed and running — a no-op otherwise. Best-effort:
// the caller logs and moves on if this fails, since the LDAP account/group
// (the thing that actually matters) already exists by the time this runs.
func (s *Server) ensureWebdavFolders(ctx context.Context, names ...string) error {
	status, ok, err := s.Modules.GetInstalled(ctx, "fileshare-webdav")
	if err != nil {
		return err
	}
	if !ok || status.Status != "running" {
		return nil
	}
	addr := s.Modules.ServiceAddr("fileshare-webdav", "webdav", 6065)
	client := webdavfs.NewClient("http://"+addr, status.Config["admin_username"], status.Config["admin_password"])
	for _, name := range names {
		if err := client.EnsureFolder(ctx, name); err != nil {
			return fmt.Errorf("ensure folder %s: %w", name, err)
		}
	}
	return nil
}

// webdavGroupContext gathers everything needed to (re)build WebDAV
// per-user/group access rules: every LDAP username, which group each
// belongs to, and every group name that exists.
func webdavGroupContext(client *directory.Client) (usernames []string, groupOf map[string]string, groupNames []string, err error) {
	users, err := client.ListUsers()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list users: %w", err)
	}
	groups, err := client.ListGroups()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list groups: %w", err)
	}
	groupOf = make(map[string]string, len(users))
	groupNames = make([]string, 0, len(groups))
	for _, g := range groups {
		groupNames = append(groupNames, g.Name)
		for _, m := range g.Members {
			groupOf[m] = g.Name
		}
	}
	return usernamesOf(users), groupOf, groupNames, nil
}

// syncWebdavLogin gives a user a WebDAV login matching their current LDAP
// password (or updates it) and rebuilds everyone's folder-access rules —
// no-op if the WebDAV module isn't installed. Best-effort, same reasoning
// as ensureWebdavFolders: the LDAP account is what actually matters, this
// is a convenience on top of it.
func (s *Server) syncWebdavLogin(ctx context.Context, username, password string, usernames []string, groupOf map[string]string, groupNames []string) error {
	status, ok, err := s.Modules.GetInstalled(ctx, "fileshare-webdav")
	if err != nil {
		return err
	}
	if !ok || status.Status != "running" {
		return nil
	}
	volume := s.Modules.VolumeName("fileshare-webdav", "webdav_config")
	container := s.Modules.ContainerName("fileshare-webdav", "webdav")
	return webdavfs.SyncUser(ctx, s.Docker, volume, container, username, password, usernames, groupOf, groupNames)
}

// backfillWebdavFolders waits for a freshly-installed fileshare-webdav to
// come up, then creates a folder for every LDAP user and group that
// already existed before the module was installed — see the install
// handler's comment on why this is needed at all. Best-effort and
// deliberately doesn't touch WebDAV logins/passwords: we never persist a
// user's plaintext password after their LDAP account is created, so
// there's no correct value to backfill a login with — an existing user's
// login only gets created/repaired the next time something touches their
// account (password reset, group change), same as before this existed.
func (s *Server) backfillWebdavFolders(ctx context.Context) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		status, ok, err := s.Modules.GetInstalled(ctx, "fileshare-webdav")
		if err == nil && ok && status.Status == "running" {
			break
		}
		time.Sleep(3 * time.Second)
	}

	dirClient, dirAvailable, err := s.directoryClient(ctx)
	if err != nil || !dirAvailable {
		return // no LDAP module, or it errored — nothing to back-fill
	}
	usernames, _, groupNames, err := webdavGroupContext(dirClient)
	if err != nil {
		log.Printf("webdav folder backfill: list directory: %v", err)
		return
	}

	names := append([]string{"shared"}, usernames...)
	names = append(names, groupNames...)
	if err := s.ensureWebdavFolders(ctx, names...); err != nil {
		log.Printf("webdav folder backfill: %v", err)
	}
}

// rebuildWebdavRules re-applies folder-access rules for everyone without
// touching any passwords — for when group structure changes (a group is
// created/deleted, or someone moves to a different group).
func (s *Server) rebuildWebdavRules(ctx context.Context, usernames []string, groupOf map[string]string, groupNames []string) error {
	status, ok, err := s.Modules.GetInstalled(ctx, "fileshare-webdav")
	if err != nil {
		return err
	}
	if !ok || status.Status != "running" {
		return nil
	}
	volume := s.Modules.VolumeName("fileshare-webdav", "webdav_config")
	container := s.Modules.ContainerName("fileshare-webdav", "webdav")
	return webdavfs.RebuildRules(ctx, s.Docker, volume, container, usernames, groupOf, groupNames)
}

func (s *Server) removeWebdavLogin(ctx context.Context, username string) error {
	status, ok, err := s.Modules.GetInstalled(ctx, "fileshare-webdav")
	if err != nil {
		return err
	}
	if !ok || status.Status != "running" {
		return nil
	}
	volume := s.Modules.VolumeName("fileshare-webdav", "webdav_config")
	container := s.Modules.ContainerName("fileshare-webdav", "webdav")
	return webdavfs.RemoveUser(ctx, s.Docker, volume, container, username)
}

func usernamesOf(users []directory.User) []string {
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = u.Username
	}
	return names
}

type ListUsersInput struct {
	SessionToken string `cookie:"itp_session"`
}

type UserOut struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	// Group is "" if the user somehow isn't in any group (shouldn't
	// happen going forward — group is required at creation — but existing
	// directories predating this feature may have ungrouped users).
	Group    string `json:"group"`
	Disabled bool   `json:"disabled"`
}

// disabledUsernames returns the set of usernames currently marked
// disabled, per our own tracking table (see migration 0003 for why this
// isn't just an LDAP attribute).
func (s *Server) disabledUsernames(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.Query(ctx, `SELECT username FROM disabled_users`)
	if err != nil {
		return nil, fmt.Errorf("query disabled_users: %w", err)
	}
	defer rows.Close()

	disabled := make(map[string]bool)
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, fmt.Errorf("scan disabled_users: %w", err)
		}
		disabled[username] = true
	}
	return disabled, rows.Err()
}

// groupOf builds a username -> group name lookup from the full group
// list, for annotating each user with which group they're in.
func groupOf(groups []GroupOut) map[string]string {
	m := make(map[string]string)
	for _, g := range groups {
		for _, member := range g.Members {
			m[member] = g.Name
		}
	}
	return m
}

type ListUsersOutput struct {
	Body struct {
		// Available is false until the Identity module is installed and
		// running; the frontend shows a prompt to install it instead of
		// an empty user list.
		Available bool      `json:"available"`
		Users     []UserOut `json:"users"`
	}
}

type CreateUserInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Group    string `json:"group"`
	}
}

type CreateUserOutput struct {
	Body struct {
		Success bool `json:"success"`
		// Password is what actually got set — either what was submitted,
		// or a freshly generated one if that was left blank. The admin
		// needs to see it once to hand it to the new user.
		Password string `json:"password"`
	}
}

type UsernameInput struct {
	SessionToken string `cookie:"itp_session"`
	Username     string `path:"username"`
}

type UpdateUserInput struct {
	SessionToken string `cookie:"itp_session"`
	Username     string `path:"username"`
	Body         struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
}

type ChangeGroupInput struct {
	SessionToken string `cookie:"itp_session"`
	Username     string `path:"username"`
	Body         struct {
		Group string `json:"group"`
	}
}

type ResetPasswordInput struct {
	SessionToken string `cookie:"itp_session"`
	Username     string `path:"username"`
	Body         struct {
		Password string `json:"password"`
	}
}

type ResetPasswordOutput struct {
	Body struct {
		Success  bool   `json:"success"`
		Password string `json:"password"`
	}
}

func registerUsers(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-users",
		Method:      "GET",
		Path:        "/api/users",
		Summary:     "List company users (requires the Identity module)",
	}, func(ctx context.Context, in *ListUsersInput) (*ListUsersOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		out := &ListUsersOutput{}
		out.Body.Available = available
		if !available {
			return out, nil
		}
		users, err := client.ListUsers()
		if err != nil {
			return nil, internalError("list users", err)
		}
		groups, err := client.ListGroups()
		if err != nil {
			return nil, internalError("list groups", err)
		}
		groupOut := make([]GroupOut, 0, len(groups))
		for _, g := range groups {
			groupOut = append(groupOut, GroupOut{Name: g.Name, Members: g.Members})
		}
		byUser := groupOf(groupOut)
		disabled, err := s.disabledUsernames(ctx)
		if err != nil {
			return nil, internalError("list disabled users", err)
		}
		for _, u := range users {
			out.Body.Users = append(out.Body.Users, UserOut{
				Username: u.Username, Email: u.Email, Name: u.Name,
				Group: byUser[u.Username], Disabled: disabled[u.Username],
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-user",
		Method:      "POST",
		Path:        "/api/users",
		Summary:     "Create a company user",
	}, func(ctx context.Context, in *CreateUserInput) (*CreateUserOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		if in.Body.Group == "" {
			return nil, huma.Error400BadRequest("group is required")
		}
		groups, err := client.ListGroups()
		if err != nil {
			return nil, internalError("list groups", err)
		}
		groupExists := false
		for _, g := range groups {
			if g.Name == in.Body.Group {
				groupExists = true
				break
			}
		}
		if !groupExists {
			return nil, huma.Error400BadRequest("unknown group " + in.Body.Group)
		}
		used, err := client.CreateUser(in.Body.Username, in.Body.Email, in.Body.Name, in.Body.Password)
		if err != nil {
			return nil, huma.Error400BadRequest("create user failed", err)
		}
		if err := client.AddUserToGroup(in.Body.Username, in.Body.Group); err != nil {
			return nil, internalError("user created but failed to add to group", err)
		}
		if err := s.ensureWebdavFolders(ctx, "shared", in.Body.Username); err != nil {
			log.Printf("create webdav folders for %s: %v", in.Body.Username, err)
		}
		if usernames, groupOf, groupNames, err := webdavGroupContext(client); err != nil {
			log.Printf("gather webdav group context: %v", err)
		} else if err := s.syncWebdavLogin(ctx, in.Body.Username, used, usernames, groupOf, groupNames); err != nil {
			log.Printf("sync webdav login for %s: %v", in.Body.Username, err)
		}
		out := &CreateUserOutput{}
		out.Body.Success = true
		out.Body.Password = used
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-user",
		Method:      "PATCH",
		Path:        "/api/users/{username}",
		Summary:     "Update a company user's name/email",
	}, func(ctx context.Context, in *UpdateUserInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		if err := client.UpdateUser(in.Username, in.Body.Email, in.Body.Name); err != nil {
			return nil, huma.Error400BadRequest("update user failed", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "change-user-group",
		Method:      "POST",
		Path:        "/api/users/{username}/group",
		Summary:     "Move a company user to a different group",
	}, func(ctx context.Context, in *ChangeGroupInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		if in.Body.Group == "" {
			return nil, huma.Error400BadRequest("group is required")
		}
		groups, err := client.ListGroups()
		if err != nil {
			return nil, internalError("list groups", err)
		}
		groupExists := false
		for _, g := range groups {
			for _, member := range g.Members {
				if member == in.Username && g.Name != in.Body.Group {
					if err := client.RemoveUserFromGroup(in.Username, g.Name); err != nil {
						return nil, internalError("remove from old group failed", err)
					}
				}
			}
			if g.Name == in.Body.Group {
				groupExists = true
			}
		}
		if !groupExists {
			return nil, huma.Error400BadRequest("unknown group " + in.Body.Group)
		}
		if err := client.AddUserToGroup(in.Username, in.Body.Group); err != nil {
			return nil, huma.Error400BadRequest("add to group failed", err)
		}
		if usernames, groupOf, groupNames, err := webdavGroupContext(client); err != nil {
			log.Printf("gather webdav group context: %v", err)
		} else if err := s.rebuildWebdavRules(ctx, usernames, groupOf, groupNames); err != nil {
			log.Printf("rebuild webdav rules after moving %s to %s: %v", in.Username, in.Body.Group, err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "reset-user-password",
		Method:      "POST",
		Path:        "/api/users/{username}/reset-password",
		Summary:     "Reset a company user's password",
	}, func(ctx context.Context, in *ResetPasswordInput) (*ResetPasswordOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		used, err := client.ResetPassword(in.Username, in.Body.Password)
		if err != nil {
			return nil, huma.Error400BadRequest("reset password failed", err)
		}
		if usernames, groupOf, groupNames, err := webdavGroupContext(client); err != nil {
			log.Printf("gather webdav group context: %v", err)
		} else if err := s.syncWebdavLogin(ctx, in.Username, used, usernames, groupOf, groupNames); err != nil {
			log.Printf("sync webdav login for %s: %v", in.Username, err)
		}
		out := &ResetPasswordOutput{}
		out.Body.Success = true
		out.Body.Password = used
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      "DELETE",
		Path:        "/api/users/{username}",
		Summary:     "Delete a company user",
	}, func(ctx context.Context, in *UsernameInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		// OpenLDAP doesn't automatically clean up a group's `member`
		// reference when the member entry is deleted (verified: refint is
		// loaded but not configured for this attribute) — do it ourselves
		// first, before the user (and thus the ability to identify their
		// DN) is gone.
		if groups, err := client.ListGroups(); err != nil {
			log.Printf("list groups before deleting %s: %v", in.Username, err)
		} else {
			for _, g := range groups {
				for _, member := range g.Members {
					if member == in.Username {
						if err := client.RemoveUserFromGroup(in.Username, g.Name); err != nil {
							log.Printf("remove %s from group %s: %v", in.Username, g.Name, err)
						}
					}
				}
			}
		}
		if err := client.DeleteUser(in.Username); err != nil {
			return nil, huma.Error400BadRequest("delete user failed", err)
		}
		if err := s.removeWebdavLogin(ctx, in.Username); err != nil {
			log.Printf("remove webdav login for %s: %v", in.Username, err)
		}
		if _, err := s.DB.Exec(ctx, `DELETE FROM disabled_users WHERE username = $1`, in.Username); err != nil {
			log.Printf("clear disabled flag for deleted user %s: %v", in.Username, err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "disable-user",
		Method:      "POST",
		Path:        "/api/users/{username}/disable",
		Summary:     "Disable a company user without deleting their account",
	}, func(ctx context.Context, in *UsernameInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		// Revoke their ability to authenticate by resetting their LDAP
		// password to a value nobody is shown, and dropping their WebDAV
		// login — their account, group membership, and files are untouched.
		if _, err := client.ResetPassword(in.Username, ""); err != nil {
			return nil, huma.Error400BadRequest("disable user failed", err)
		}
		if err := s.removeWebdavLogin(ctx, in.Username); err != nil {
			log.Printf("remove webdav login for disabled user %s: %v", in.Username, err)
		}
		if _, err := s.DB.Exec(ctx, `INSERT INTO disabled_users (username) VALUES ($1) ON CONFLICT DO NOTHING`, in.Username); err != nil {
			return nil, internalError("record disabled state", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "enable-user",
		Method:      "POST",
		Path:        "/api/users/{username}/enable",
		Summary:     "Re-enable a disabled company user with a fresh password",
	}, func(ctx context.Context, in *UsernameInput) (*ResetPasswordOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		client, available, err := s.directoryClient(ctx)
		if err != nil {
			return nil, internalError("check identity module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Identity module first")
		}
		// We never stored their old password (disabling reset it to
		// something nobody was shown), so re-enabling means issuing a
		// fresh one rather than restoring the old.
		used, err := client.ResetPassword(in.Username, "")
		if err != nil {
			return nil, huma.Error400BadRequest("enable user failed", err)
		}
		if usernames, groupOf, groupNames, err := webdavGroupContext(client); err != nil {
			log.Printf("gather webdav group context: %v", err)
		} else if err := s.syncWebdavLogin(ctx, in.Username, used, usernames, groupOf, groupNames); err != nil {
			log.Printf("sync webdav login for re-enabled user %s: %v", in.Username, err)
		}
		if _, err := s.DB.Exec(ctx, `DELETE FROM disabled_users WHERE username = $1`, in.Username); err != nil {
			return nil, internalError("clear disabled state", err)
		}
		out := &ResetPasswordOutput{}
		out.Body.Success = true
		out.Body.Password = used
		return out, nil
	})
}
