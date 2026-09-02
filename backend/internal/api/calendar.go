package api

import (
	"context"
	"log"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"it-platform/backend/internal/caldavclient"
	"it-platform/backend/internal/radicalefs"
)

// bootstrapCalendar runs once, right after calendar-radicale's container
// starts — same shape as bootstrapComputeMesh: poll until the module
// reaches "running" (its own config, including the generated
// service_password, only exists once it does), then seed Radicale's two
// internal accounts and the shared company calendar. See
// radicalefs.Bootstrap's doc comment for why that specifically needs to
// happen now rather than lazily on first use.
func (s *Server) bootstrapCalendar(ctx context.Context) {
	deadline := time.Now().Add(2 * time.Minute)
	var config map[string]string
	for time.Now().Before(deadline) {
		st, ok, err := s.Modules.GetInstalled(ctx, "calendar-radicale")
		if err == nil && ok && st.Status == "running" {
			config = st.Config
			break
		}
		time.Sleep(3 * time.Second)
	}
	if config == nil {
		log.Printf("calendar bootstrap: module never reached running, giving up")
		return
	}

	volume := s.Modules.VolumeName("calendar-radicale", "radicale_config")
	container := s.Modules.ContainerName("calendar-radicale", "radicale")
	addr := s.Modules.ServiceAddr("calendar-radicale", "radicale", 5232)
	baseURL := "http://" + addr

	if err := radicalefs.Bootstrap(ctx, s.Docker, volume, container, baseURL, config["service_password"]); err != nil {
		log.Printf("calendar bootstrap: %v", err)
		return
	}
	log.Printf("calendar bootstrap: complete")
}

// calendarAccess resolves whether calendar-radicale is installed and
// running, and if so the base URL and internal service credentials the
// backend uses for every portal-mediated event read/write — the same
// "platform-service" account for every employee, never their own
// personal Radicale login (that login exists only for native calendar
// app subscriptions, a path this backend never takes itself).
func (s *Server) calendarAccess(ctx context.Context) (baseURL, servicePassword string, available bool, err error) {
	status, ok, err := s.Modules.GetInstalled(ctx, "calendar-radicale")
	if err != nil {
		return "", "", false, err
	}
	if !ok || status.Status != "running" {
		return "", "", false, nil
	}
	servicePassword = status.Config["service_password"]
	if servicePassword == "" {
		return "", "", false, nil
	}
	addr := s.Modules.ServiceAddr("calendar-radicale", "radicale", 5232)
	return "http://" + addr, servicePassword, true, nil
}

// ensurePersonalCalendar provisions an employee's personal calendar if
// it's missing — see its one call site in portal.go's login handler for
// why login is the only place this can happen for a pre-existing
// employee. Best-effort and silent on any failure short of a real bug:
// the calendar module might not even be installed, which is the normal
// case for most deployments and logins.
func (s *Server) ensurePersonalCalendar(ctx context.Context, username, password string) {
	baseURL, servicePassword, available, err := s.calendarAccess(ctx)
	if err != nil || !available {
		return
	}
	path := radicalefs.PersonalCalendarPath(username)
	exists, err := caldavclient.CalendarExists(ctx, baseURL, radicalefs.ServiceAccount, servicePassword, path)
	if err != nil {
		log.Printf("check personal calendar for %s: %v", username, err)
		return
	}
	if exists {
		return
	}
	if err := s.syncCalendarLogin(ctx, username, password); err != nil {
		log.Printf("provision personal calendar for %s: %v", username, err)
	}
}

// validVideoCallURL rejects anything but a plain http(s) link — any
// employee can set this field on a company-calendar event another
// employee will later see rendered as a clickable link (Calendar.vue's
// edit modal), so an unvalidated value here is a stored XSS vector via a
// javascript: URI. Empty is fine (no video call attached).
func validVideoCallURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// calendarPathFor maps this API's "company"|"personal" calendar
// selector to a real Radicale path — "personal" always resolves to the
// calling employee's own calendar, derived from their session, never a
// client-supplied username, so there's no way to reach anyone else's
// personal calendar through this endpoint.
func calendarPathFor(kind, username string) (path string, ok bool) {
	switch kind {
	case "company":
		return radicalefs.CompanyCalendarPath, true
	case "personal":
		return radicalefs.PersonalCalendarPath(username), true
	default:
		return "", false
	}
}

type CalendarEventOut struct {
	UID          string    `json:"uid"`
	Calendar     string    `json:"calendar"` // "company" or "personal"
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	AllDay       bool      `json:"all_day"`
	CreatedBy    string    `json:"created_by,omitempty"`
	Attendees    []string  `json:"attendees,omitempty"`
	VideoCallURL string    `json:"video_call_url,omitempty"`
}

type ListCalendarEventsInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Start        string `query:"start"`
	End          string `query:"end"`
}

type ListCalendarEventsOutput struct {
	Body struct {
		Available bool               `json:"available"`
		Events    []CalendarEventOut `json:"events"`
	}
}

type CalendarEventBody struct {
	Calendar     string    `json:"calendar"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	AllDay       bool      `json:"all_day"`
	Attendees    []string  `json:"attendees,omitempty"`
	VideoCallURL string    `json:"video_call_url,omitempty"`
}

type CreateCalendarEventInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Body         CalendarEventBody
}

type CreateCalendarEventOutput struct {
	Body struct {
		UID string `json:"uid"`
	}
}

type UpdateCalendarEventInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Calendar     string `path:"calendar"`
	UID          string `path:"uid"`
	Body         CalendarEventBody
}

type DeleteCalendarEventInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Calendar     string `path:"calendar"`
	UID          string `path:"uid"`
}

type ListDirectoryUsersInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type DirectoryUserOut struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

type ListDirectoryUsersOutput struct {
	Body struct {
		Users []DirectoryUserOut `json:"users"`
	}
}

func registerCalendar(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-directory-users-portal",
		Method:      "GET",
		Path:        "/api/portal/directory/users",
		Summary:     "List company employees (for picking event attendees) — usernames and display names only",
	}, func(ctx context.Context, in *ListDirectoryUsersInput) (*ListDirectoryUsersOutput, error) {
		if _, err := s.requireEmployeeAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		out := &ListDirectoryUsersOutput{}
		out.Body.Users = []DirectoryUserOut{}
		dirClient, available, err := s.directoryClient(ctx)
		if err != nil || !available {
			return out, nil
		}
		users, err := dirClient.ListUsers()
		if err != nil {
			return nil, internalError("list directory users", err)
		}
		for _, u := range users {
			out.Body.Users = append(out.Body.Users, DirectoryUserOut{Username: u.Username, Name: u.Name})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-calendar-events",
		Method:      "GET",
		Path:        "/api/portal/calendar/events",
		Summary:     "List company + own personal calendar events in a date range",
	}, func(ctx context.Context, in *ListCalendarEventsInput) (*ListCalendarEventsOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		out := &ListCalendarEventsOutput{}
		out.Body.Events = []CalendarEventOut{}
		baseURL, password, available, err := s.calendarAccess(ctx)
		if err != nil {
			return nil, internalError("check calendar module", err)
		}
		out.Body.Available = available
		if !available {
			return out, nil
		}
		start, err := time.Parse(time.RFC3339, in.Start)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid start")
		}
		end, err := time.Parse(time.RFC3339, in.End)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid end")
		}

		for _, kind := range []string{"company", "personal"} {
			path, _ := calendarPathFor(kind, username)
			events, err := caldavclient.ListEvents(ctx, baseURL, radicalefs.ServiceAccount, password, path, start, end)
			if err != nil {
				return nil, internalError("list "+kind+" calendar events", err)
			}
			for _, ev := range events {
				// Our own create/update handlers already reject anything
				// but a plain http(s) URL here — but the company calendar
				// is also writable directly via CalDAV (any employee's own
				// Radicale login, see radicalefs.SyncUser), which has no
				// idea this field exists let alone that it should be
				// scheme-restricted. Re-checking on the way out is what
				// actually closes the javascript:-URI stored-XSS path,
				// not the write-side check alone.
				videoCallURL := ev.VideoCallURL
				if !validVideoCallURL(videoCallURL) {
					videoCallURL = ""
				}
				out.Body.Events = append(out.Body.Events, CalendarEventOut{
					UID: ev.UID, Calendar: kind, Title: ev.Title, Description: ev.Description,
					Start: ev.Start, End: ev.End, AllDay: ev.AllDay, CreatedBy: ev.CreatedBy,
					Attendees: ev.Attendees, VideoCallURL: videoCallURL,
				})
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-calendar-event",
		Method:      "POST",
		Path:        "/api/portal/calendar/events",
		Summary:     "Create an event on the company calendar or the employee's own personal calendar",
	}, func(ctx context.Context, in *CreateCalendarEventInput) (*CreateCalendarEventOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		baseURL, password, available, err := s.calendarAccess(ctx)
		if err != nil {
			return nil, internalError("check calendar module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("the Calendar module isn't available")
		}
		path, ok := calendarPathFor(in.Body.Calendar, username)
		if !ok {
			return nil, huma.Error400BadRequest(`calendar must be "company" or "personal"`)
		}
		if in.Body.Title == "" {
			return nil, huma.Error400BadRequest("title is required")
		}
		if !validVideoCallURL(in.Body.VideoCallURL) {
			return nil, huma.Error400BadRequest("video call link must be a plain http(s) URL")
		}
		uid := uuid.NewString()
		ev := caldavclient.Event{
			UID: uid, Title: in.Body.Title, Description: in.Body.Description,
			Start: in.Body.Start, End: in.Body.End, AllDay: in.Body.AllDay, CreatedBy: username,
			Attendees: in.Body.Attendees, VideoCallURL: in.Body.VideoCallURL,
		}
		if err := caldavclient.PutEvent(ctx, baseURL, radicalefs.ServiceAccount, password, path, ev); err != nil {
			return nil, internalError("create event", err)
		}
		out := &CreateCalendarEventOutput{}
		out.Body.UID = uid
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-calendar-event",
		Method:      "PATCH",
		Path:        "/api/portal/calendar/events/{calendar}/{uid}",
		Summary:     "Update an event",
	}, func(ctx context.Context, in *UpdateCalendarEventInput) (*ModuleActionOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		baseURL, password, available, err := s.calendarAccess(ctx)
		if err != nil {
			return nil, internalError("check calendar module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("the Calendar module isn't available")
		}
		path, ok := calendarPathFor(in.Calendar, username)
		if !ok {
			return nil, huma.Error400BadRequest(`calendar must be "company" or "personal"`)
		}
		if in.Body.Title == "" {
			return nil, huma.Error400BadRequest("title is required")
		}
		if !validVideoCallURL(in.Body.VideoCallURL) {
			return nil, huma.Error400BadRequest("video call link must be a plain http(s) URL")
		}
		// PUT replaces the whole resource, so the original creator has to
		// be read back first or it's lost — falls back to whoever's
		// editing now for an event that predates this field (or one a
		// native CalDAV client created, which never sets it at all).
		createdBy := username
		if existing, err := caldavclient.GetEvent(ctx, baseURL, radicalefs.ServiceAccount, password, path, in.UID); err == nil && existing != nil && existing.CreatedBy != "" {
			createdBy = existing.CreatedBy
		}
		ev := caldavclient.Event{
			UID: in.UID, Title: in.Body.Title, Description: in.Body.Description,
			Start: in.Body.Start, End: in.Body.End, AllDay: in.Body.AllDay, CreatedBy: createdBy,
			Attendees: in.Body.Attendees, VideoCallURL: in.Body.VideoCallURL,
		}
		if err := caldavclient.PutEvent(ctx, baseURL, radicalefs.ServiceAccount, password, path, ev); err != nil {
			return nil, internalError("update event", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-calendar-event",
		Method:      "DELETE",
		Path:        "/api/portal/calendar/events/{calendar}/{uid}",
		Summary:     "Delete an event",
	}, func(ctx context.Context, in *DeleteCalendarEventInput) (*ModuleActionOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		baseURL, password, available, err := s.calendarAccess(ctx)
		if err != nil {
			return nil, internalError("check calendar module", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("the Calendar module isn't available")
		}
		path, ok := calendarPathFor(in.Calendar, username)
		if !ok {
			return nil, huma.Error400BadRequest(`calendar must be "company" or "personal"`)
		}
		if err := caldavclient.DeleteEvent(ctx, baseURL, radicalefs.ServiceAccount, password, path, in.UID); err != nil {
			return nil, internalError("delete event", err)
		}
		out := &ModuleActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}
