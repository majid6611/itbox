// Package caldavclient wraps the calendar-radicale module's CalDAV
// engine (Radicale) for the handful of operations this platform's own
// portal calendar needs: list events in a date range, create/update one,
// delete one, and create a new calendar collection. Built on
// github.com/emersion/go-webdav (RFC 4791 CalDAV client) rather than
// hand-rolled XML/iCalendar handling — unlike MeshCentral's control
// protocol earlier this session, a real, actively maintained Go client
// library exists for CalDAV, so there's no reason to reinvent it.
//
// Every behavior this package relies on (bcrypt-hashed htpasswd auth,
// the rights file's exact shape, MKCALENDAR's 409-on-exists semantics,
// DTSTAMP being mandatory) was confirmed against a real running Radicale
// container before being written here, not assumed from documentation
// alone — see the calendar-radicale module's own notes.
package caldavclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

// propCreatedBy is a custom iCalendar X-property (the standard extension
// mechanism RFC 5545 itself defines — any name prefixed "X-" is fair
// game) recording which employee created an event, so the company
// calendar can color it by owner. Not one of go-ical's named constants
// since it isn't a standard property; Props.Text/SetText work on any
// property name regardless.
const propCreatedBy = "X-ITBOX-CREATED-BY"

type Event struct {
	UID         string
	Title       string
	Description string
	Start       time.Time
	End         time.Time
	AllDay      bool
	CreatedBy   string
	// Attendees holds employee usernames, encoded as real ATTENDEE
	// properties (CN=username, a placeholder mailto: URI as the value —
	// ATTENDEE requires one, and username is this platform's identity
	// either way, not email) rather than a custom X-property: unlike
	// CreatedBy, this is standard iCalendar data a native CalDAV client
	// can already display meaningfully.
	Attendees []string
	// VideoCallURL is a video-jitsi room link, stored as the standard
	// VEVENT URL property — native CalDAV clients already know to surface
	// a event's URL as a tappable link, so this needs no custom encoding
	// either.
	VideoCallURL string
}

func newHTTPClient(username, password string) *http.Client {
	return &http.Client{Transport: basicAuthTransport{username: username, password: password}}
}

type basicAuthTransport struct {
	username, password string
}

func (t basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.username, t.password)
	return http.DefaultTransport.RoundTrip(req)
}

func newCalDAVClient(baseURL, username, password string) (*caldav.Client, error) {
	hc := webdav.HTTPClientWithBasicAuth(newHTTPClient(username, password), username, password)
	return caldav.NewClient(hc, baseURL)
}

// isNotFound detects a 404 from go-webdav's own internal error type, which
// isn't exported (confirmed live: QueryCalendar against a missing
// collection returns a *internal.HTTPError this package can't name or
// type-assert against) — its Error() string reliably starts with the
// status line, which is the only thing left to match on.
func isNotFound(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "404 ")
}

// CalendarExists checks whether a calendar collection is already there —
// cheap (one PROPFIND, no write, no container restart), so callers that
// only need to provision something once (see the portal login handler)
// can skip the expensive path entirely once it's already done.
func CalendarExists(ctx context.Context, baseURL, authUser, authPass, calendarPath string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", baseURL+calendarPath, nil)
	if err != nil {
		return false, fmt.Errorf("build propfind request: %w", err)
	}
	req.Header.Set("Depth", "0")
	resp, err := newHTTPClient(authUser, authPass).Do(req)
	if err != nil {
		return false, fmt.Errorf("propfind %s: %w", calendarPath, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusMultiStatus, http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("propfind %s: unexpected status %d", calendarPath, resp.StatusCode)
	}
}

// EnsureCalendarAsOwner creates a calendar collection, authenticating AS
// the collection's own owner (username/password) rather than some other
// elevated account. This isn't optional: confirmed live that Radicale
// refuses to auto-vivify a principal's home collection ("/<user>/") for
// anyone but that principal itself, even for an account whose rights
// file entry otherwise grants it blanket access — a request from
// elsewhere gets a bare 409 with no rights-denial logged, which reads
// exactly like "already exists" unless you've seen the working case too.
// A 409 here is genuinely ambiguous between the two, so it's always
// treated as success — the one time that's wrong (some other real
// conflict) resurfaces immediately on the very next write anyway.
func EnsureCalendarAsOwner(ctx context.Context, baseURL, username, password, calendarPath, displayName string) error {
	hc := newHTTPClient(username, password)
	req, err := http.NewRequestWithContext(ctx, "MKCALENDAR", baseURL+calendarPath, nil)
	if err != nil {
		return fmt.Errorf("build mkcalendar request: %w", err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("mkcalendar %s: %w", calendarPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("mkcalendar %s: unexpected status %d", calendarPath, resp.StatusCode)
}

// ListEvents fetches every VEVENT in calendarPath whose time range
// overlaps [start, end) — a plain REPORT calendar-query, the standard
// CalDAV way to ask "what happens in this window" without fetching every
// event ever created.
func ListEvents(ctx context.Context, baseURL, authUser, authPass, calendarPath string, start, end time.Time) ([]Event, error) {
	client, err := newCalDAVClient(baseURL, authUser, authPass)
	if err != nil {
		return nil, err
	}
	query := &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name: "VCALENDAR",
			Comps: []caldav.CalendarCompRequest{{
				Name:  "VEVENT",
				Props: []string{"UID", "SUMMARY", "DESCRIPTION", "DTSTART", "DTEND", propCreatedBy, "ATTENDEE", "URL"},
			}},
		},
		CompFilter: caldav.CompFilter{
			Name:  "VCALENDAR",
			Comps: []caldav.CompFilter{{Name: "VEVENT", Start: start, End: end}},
		},
	}
	objs, err := client.QueryCalendar(ctx, calendarPath, query)
	if err != nil {
		if isNotFound(err) {
			// The collection itself doesn't exist yet — e.g. an employee
			// who existed before this module was installed and hasn't
			// logged into the portal since (see portal.go's login
			// handler, which is what actually creates it). "No calendar
			// yet" reads the same as "no events in it" to every caller
			// here, so this degrades instead of failing the whole
			// request — a company calendar's real events shouldn't
			// disappear behind one employee's personal calendar being
			// unprovisioned.
			return nil, nil
		}
		return nil, fmt.Errorf("query calendar %s: %w", calendarPath, err)
	}

	var events []Event
	for _, obj := range objs {
		if obj.Data == nil {
			continue
		}
		for _, child := range obj.Data.Children {
			if child.Name != ical.CompEvent {
				continue
			}
			ev := Event{}
			ev.UID, _ = child.Props.Text(ical.PropUID)
			ev.Title, _ = child.Props.Text(ical.PropSummary)
			ev.Description, _ = child.Props.Text(ical.PropDescription)
			ev.CreatedBy, _ = child.Props.Text(propCreatedBy)
			ev.VideoCallURL, _ = child.Props.Text(ical.PropURL)
			for _, att := range child.Props.Values(ical.PropAttendee) {
				if cn := att.Params.Get(ical.ParamCommonName); cn != "" {
					ev.Attendees = append(ev.Attendees, cn)
				}
			}
			if dtStart := child.Props.Get(ical.PropDateTimeStart); dtStart != nil {
				ev.AllDay = dtStart.Params.Get(ical.ParamValue) == string(ical.ValueDate)
			}
			ev.Start, _ = child.Props.DateTime(ical.PropDateTimeStart, time.UTC)
			ev.End, _ = child.Props.DateTime(ical.PropDateTimeEnd, time.UTC)
			if ev.UID != "" {
				events = append(events, ev)
			}
		}
	}
	return events, nil
}

// GetEvent fetches one event by UID, or (nil, nil) if it doesn't exist —
// used to read back CreatedBy before an update overwrites the resource,
// since PUT replaces the whole thing and there'd otherwise be nowhere
// left to carry that value forward from.
func GetEvent(ctx context.Context, baseURL, authUser, authPass, calendarPath, uid string) (*Event, error) {
	client, err := newCalDAVClient(baseURL, authUser, authPass)
	if err != nil {
		return nil, err
	}
	obj, err := client.GetCalendarObject(ctx, calendarPath+uid+".ics")
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get event %s: %w", uid, err)
	}
	if obj.Data == nil {
		return nil, nil
	}
	for _, child := range obj.Data.Children {
		if child.Name != ical.CompEvent {
			continue
		}
		ev := &Event{}
		ev.UID, _ = child.Props.Text(ical.PropUID)
		ev.CreatedBy, _ = child.Props.Text(propCreatedBy)
		return ev, nil
	}
	return nil, nil
}

// PutEvent creates a new event (ev.UID freshly generated by the caller)
// or overwrites an existing one at the same UID — CalDAV's PUT is the
// same verb for both, keyed by the resource's path.
func PutEvent(ctx context.Context, baseURL, authUser, authPass, calendarPath string, ev Event) error {
	client, err := newCalDAVClient(baseURL, authUser, authPass)
	if err != nil {
		return err
	}

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//itbox//calendar//EN")
	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, ev.UID)
	// DTSTAMP is mandatory on every VEVENT (RFC 5545) — go-ical refuses
	// to encode one without it, confirmed live, not assumed from the
	// spec text alone.
	dtstamp := ical.NewProp(ical.PropDateTimeStamp)
	dtstamp.SetDateTime(time.Now().UTC())
	event.Props.Set(dtstamp)
	event.Props.SetText(ical.PropSummary, ev.Title)
	if ev.Description != "" {
		event.Props.SetText(ical.PropDescription, ev.Description)
	}
	if ev.CreatedBy != "" {
		event.Props.SetText(propCreatedBy, ev.CreatedBy)
	}
	if ev.VideoCallURL != "" {
		event.Props.SetText(ical.PropURL, ev.VideoCallURL)
	}
	for _, username := range ev.Attendees {
		att := ical.NewProp(ical.PropAttendee)
		att.Params.Set(ical.ParamCommonName, username)
		att.SetText("mailto:" + username + "@attendee.invalid")
		event.Props.Add(att)
	}
	if ev.AllDay {
		startProp := ical.NewProp(ical.PropDateTimeStart)
		startProp.SetDate(ev.Start)
		event.Props.Set(startProp)
		endProp := ical.NewProp(ical.PropDateTimeEnd)
		// DTEND must be strictly after DTSTART for an all-day event — it's
		// the day *after* the last day it covers, not the last day itself
		// (confirmed live: Radicale silently accepts DTEND == DTSTART, but
		// then never matches it against any day-range query at all — a
		// month/week view still drew a bar for it, day view just never
		// found it). Callers are expected to already pass a proper
		// exclusive End; this only guards the degenerate case where it
		// isn't actually after Start.
		end := ev.End
		if !end.After(ev.Start) {
			end = ev.Start.AddDate(0, 0, 1)
		}
		endProp.SetDate(end)
		event.Props.Set(endProp)
	} else {
		startProp := ical.NewProp(ical.PropDateTimeStart)
		startProp.SetDateTime(ev.Start.UTC())
		event.Props.Set(startProp)
		endProp := ical.NewProp(ical.PropDateTimeEnd)
		endProp.SetDateTime(ev.End.UTC())
		event.Props.Set(endProp)
	}
	cal.Children = append(cal.Children, event.Component)

	path := calendarPath + ev.UID + ".ics"
	if _, err := client.PutCalendarObject(ctx, path, cal); err != nil {
		return fmt.Errorf("put event %s: %w", path, err)
	}
	return nil
}

// DeleteEvent removes one event by UID.
func DeleteEvent(ctx context.Context, baseURL, authUser, authPass, calendarPath, uid string) error {
	hc := webdav.HTTPClientWithBasicAuth(newHTTPClient(authUser, authPass), authUser, authPass)
	client, err := webdav.NewClient(hc, baseURL)
	if err != nil {
		return err
	}
	if err := client.RemoveAll(ctx, calendarPath+uid+".ics"); err != nil {
		return fmt.Errorf("delete event %s: %w", uid, err)
	}
	return nil
}
