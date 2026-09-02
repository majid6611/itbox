package api

import (
	"context"
	"crypto/sha256"

	"github.com/danielgtaylor/huma/v2"
)

// defaultColorPalette is what an employee's calendar color falls back to
// until an admin picks one explicitly — stable per username (same
// employee always gets the same default) rather than random, so the
// company calendar doesn't reshuffle colors on every page load for
// anyone nobody's assigned yet.
var defaultColorPalette = []string{
	"#2563eb", "#dc2626", "#16a34a", "#9333ea", "#ea580c",
	"#0891b2", "#c026d3", "#65a30d", "#e11d48", "#4f46e5",
}

func defaultColorFor(username string) string {
	sum := sha256.Sum256([]byte(username))
	return defaultColorPalette[int(sum[0])%len(defaultColorPalette)]
}

type CalendarSettingsData struct {
	StartTime           string `json:"start_time"`
	EndTime             string `json:"end_time"`
	SlotDurationMinutes int    `json:"slot_duration_minutes"`
	// DefaultView is one of "month" | "week" | "day" — which view the
	// portal calendar opens to. Kept as this small vocabulary rather than
	// FullCalendar's own view names (dayGridMonth, ...) so the frontend's
	// choice of calendar library stays swappable without a data migration.
	DefaultView  string            `json:"default_view"`
	MemberColors map[string]string `json:"member_colors"`
}

func (s *Server) loadCalendarSettings(ctx context.Context) (CalendarSettingsData, error) {
	var data CalendarSettingsData
	err := s.DB.QueryRow(ctx, `SELECT start_time, end_time, slot_duration_minutes, default_view FROM calendar_settings WHERE id = true`).
		Scan(&data.StartTime, &data.EndTime, &data.SlotDurationMinutes, &data.DefaultView)
	if err != nil {
		// No row yet (module never had its settings touched) — sensible
		// defaults, matching the migration's own column defaults.
		data.StartTime, data.EndTime, data.SlotDurationMinutes, data.DefaultView = "07:00", "20:00", 30, "month"
	}

	data.MemberColors = make(map[string]string)
	if dirClient, available, err := s.directoryClient(ctx); err == nil && available {
		if users, err := dirClient.ListUsers(); err == nil {
			for _, u := range users {
				data.MemberColors[u.Username] = defaultColorFor(u.Username)
			}
		}
	}
	rows, err := s.DB.Query(ctx, `SELECT username, color FROM calendar_member_colors`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var username, color string
			if rows.Scan(&username, &color) == nil {
				data.MemberColors[username] = color
			}
		}
	}
	return data, nil
}

type GetCalendarSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type GetPortalCalendarSettingsInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type CalendarSettingsOutput struct {
	Body CalendarSettingsData
}

type UpdateCalendarSettingsInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		StartTime           string `json:"start_time"`
		EndTime             string `json:"end_time"`
		SlotDurationMinutes int    `json:"slot_duration_minutes"`
		DefaultView         string `json:"default_view"`
	}
}

type SetMemberColorInput struct {
	SessionToken string `cookie:"itp_session"`
	Username     string `path:"username"`
	Body         struct {
		Color string `json:"color"`
	}
}

func registerCalendarSettings(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-calendar-settings",
		Method:      "GET",
		Path:        "/api/calendar/settings",
		Summary:     "Get the calendar module's display settings (business hours, slot size, member colors)",
	}, func(ctx context.Context, in *GetCalendarSettingsInput) (*CalendarSettingsOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		data, err := s.loadCalendarSettings(ctx)
		if err != nil {
			return nil, internalError("load calendar settings", err)
		}
		return &CalendarSettingsOutput{Body: data}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-portal-calendar-settings",
		Method:      "GET",
		Path:        "/api/portal/calendar/settings",
		Summary:     "Read-only calendar display settings for the employee portal's own calendar view",
	}, func(ctx context.Context, in *GetPortalCalendarSettingsInput) (*CalendarSettingsOutput, error) {
		if _, err := s.requireEmployeeAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		data, err := s.loadCalendarSettings(ctx)
		if err != nil {
			return nil, internalError("load calendar settings", err)
		}
		return &CalendarSettingsOutput{Body: data}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-calendar-settings",
		Method:      "PUT",
		Path:        "/api/calendar/settings",
		Summary:     "Set business hours and time-slot size for the calendar views",
	}, func(ctx context.Context, in *UpdateCalendarSettingsInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if in.Body.StartTime == "" || in.Body.EndTime == "" {
			return nil, huma.Error400BadRequest("start_time and end_time are required")
		}
		switch in.Body.SlotDurationMinutes {
		case 15, 30, 45, 60:
		default:
			return nil, huma.Error400BadRequest("slot_duration_minutes must be 15, 30, 45, or 60")
		}
		switch in.Body.DefaultView {
		case "month", "week", "day":
		default:
			return nil, huma.Error400BadRequest(`default_view must be "month", "week", or "day"`)
		}
		_, err := s.DB.Exec(ctx, `
			INSERT INTO calendar_settings (id, start_time, end_time, slot_duration_minutes, default_view)
			VALUES (true, $1, $2, $3, $4)
			ON CONFLICT (id) DO UPDATE SET start_time = $1, end_time = $2, slot_duration_minutes = $3, default_view = $4
		`, in.Body.StartTime, in.Body.EndTime, in.Body.SlotDurationMinutes, in.Body.DefaultView)
		if err != nil {
			return nil, internalError("save calendar settings", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-calendar-member-color",
		Method:      "PUT",
		Path:        "/api/calendar/settings/colors/{username}",
		Summary:     "Set the color an employee's events show in on the company calendar",
	}, func(ctx context.Context, in *SetMemberColorInput) (*ModuleActionOutput, error) {
		if _, err := s.requireAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		if in.Body.Color == "" {
			return nil, huma.Error400BadRequest("color is required")
		}
		_, err := s.DB.Exec(ctx, `
			INSERT INTO calendar_member_colors (username, color) VALUES ($1, $2)
			ON CONFLICT (username) DO UPDATE SET color = $2
		`, in.Username, in.Body.Color)
		if err != nil {
			return nil, internalError("save member color", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}
