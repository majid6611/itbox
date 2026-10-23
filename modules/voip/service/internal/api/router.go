package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"it-platform/voip/internal/dbx"
	"log"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"it-platform/voip/internal/ami"
	"it-platform/voip/internal/auth"
	"it-platform/voip/internal/pbx"
)

type Server struct {
	DB  *pgxpool.Pool
	AMI *ami.Client
	// ConfigDir is the volume shared with the asterisk container, where
	// the generated pjsip/extensions/voicemail files go.
	ConfigDir string
	// SoundsDir is the volume shared with asterisk where the phone-menu
	// greeting lives (asterisk sees it at /var/lib/itp-sounds).
	SoundsDir string
	// CDRFile is Asterisk's call log (cdr_csv's Master.csv), read-only.
	CDRFile string

	// applyMu serializes regenerate-and-reload, so two saves at once
	// can't interleave writing files from two different DB snapshots.
	applyMu sync.Mutex
}

func NewRouter(s *Server) http.Handler {
	router := chi.NewMux()
	// No $schema links in responses — same as core's apiConfig (see
	// backend/internal/api/api.go for why huma's guess at them is wrong
	// behind this platform's plain-HTTP nginx).
	config := huma.DefaultConfig("Phone Module API", "1.0.0")
	config.CreateHooks = nil
	api := humachi.New(router, config)

	huma.Register(api, huma.Operation{
		OperationID: "voip-health",
		Method:      "GET",
		Path:        "/api/portal/voip/health",
		Summary:     "Health check",
	}, func(ctx context.Context, in *HealthInput) (*HealthOutput, error) {
		out := &HealthOutput{}
		out.Body.OK = true
		return out, nil
	})

	registerExtensions(api, s)
	registerSettings(api, s)
	registerIVR(api, s)
	registerCalls(api, s)
	registerPortal(api, s)

	return router
}

// internalError logs the real cause server-side and returns a generic 500
// to the client — the raw error text (a DB/AMI driver message) can reveal
// internal infrastructure details and has no reason to leave this process
// for a failure the caller didn't cause.
func internalError(msg string, err error) huma.StatusError {
	log.Printf("%s: %v", msg, err)
	dbx.SystemLog("error", "%s: %v", msg, err)
	return huma.Error500InternalServerError(msg)
}

func (s *Server) requireAdminAuth(ctx context.Context, sessionToken string) error {
	if _, err := auth.ValidateAdminSession(ctx, s.DB, sessionToken); err != nil {
		return huma.Error401Unauthorized("not authenticated")
	}
	return nil
}

func (s *Server) requireEmployeeAuth(ctx context.Context, sessionToken string) (string, error) {
	username, err := auth.ValidateEmployeeSession(ctx, s.DB, sessionToken)
	if err != nil {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	return username, nil
}

// serverAddress is the LAN IP phones should register to, as Asterisk
// itself detected it (see the asterisk image's entrypoint.sh) — "" if
// Asterisk isn't answering, and the browser falls back to its own idea.
func (s *Server) serverAddress(ctx context.Context) string {
	ip, err := s.AMI.Getvar(ctx, "ENV(SERVER_IP)")
	if err != nil {
		return ""
	}
	return ip
}

func newPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Apply regenerates Asterisk's config from the database and reloads it.
// Asterisk not answering isn't an error here: the files are already
// written, and Asterisk reads them itself whenever it next starts — the
// panel's "PBX offline" status is what tells the admin something's wrong.
func (s *Server) Apply(ctx context.Context) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	exts, err := s.loadExtensions(ctx)
	if err != nil {
		return err
	}
	st, err := s.loadSettings(ctx)
	if err != nil {
		return err
	}
	ivr, err := s.loadIVR(ctx)
	if err != nil {
		return err
	}
	if err := pbx.Write(s.ConfigDir, pbx.Render(exts, st, ivr)); err != nil {
		return err
	}
	if err := s.AMI.Command(ctx, "module reload res_pjsip.so", "dialplan reload", "voicemail reload"); err != nil {
		log.Printf("reload asterisk (config written, will apply on its next start): %v", err)
		dbx.SystemLog("warn", "Phone system config saved but Asterisk could not reload it (it will apply on the next restart): %v", err)
	}
	return nil
}

func (s *Server) loadExtensions(ctx context.Context) ([]pbx.Extension, error) {
	rows, err := s.DB.Query(ctx, `SELECT number, name, password, in_main_group FROM voip_extensions ORDER BY number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var exts []pbx.Extension
	for rows.Next() {
		var e pbx.Extension
		if err := rows.Scan(&e.Number, &e.Name, &e.Password, &e.InMainGroup); err != nil {
			return nil, err
		}
		exts = append(exts, e)
	}
	return exts, rows.Err()
}

func (s *Server) loadSettings(ctx context.Context) (pbx.Settings, error) {
	var st pbx.Settings
	err := s.DB.QueryRow(ctx, `
		SELECT trunk_enabled, trunk_host, trunk_username, trunk_password, trunk_caller_id, allow_international
		FROM voip_settings WHERE id = true
	`).Scan(&st.TrunkEnabled, &st.TrunkHost, &st.TrunkUsername, &st.TrunkPassword, &st.TrunkCallerID, &st.AllowInternational)
	return st, err
}
