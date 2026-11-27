package calendarnodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// Node type ids (forge 28b).
const (
	ParseIcsID   = "calendar.parseIcs"
	ComposeIcsID = "calendar.composeIcs"
	ReplyID      = "calendar.itipReply"
	ApplyID      = "calendar.itipApply"
)

// IcalDeps are what Parse iCalendar needs, made after the registry.
type IcalDeps struct {
	// Read parses .ics: ical.Child in a server, never ical.Parse in the
	// server's own process. Without it nothing is parsed.
	Read ical.Reader
	// Open opens a tenant's uploaded file (records/files Store.Open).
	Open func(ctx context.Context, tenant uint64, id string) (io.ReadCloser, records.FileRef, bool, error)
}

const eventDesc = "{id, title, start, end (zoned times or dates), recurrence {rrule, rdate, exdate}, recurrenceId, seriesId, status, transparency, location, videoLink, organizer {email, name}, attendees [{email, name, response, optional}], description, iCalUID, sequence}"

// ContributeIcal registers the iCalendar nodes (forge 28b); deps gives
// Parse iCalendar's dependencies once made.
func ContributeIcal(reg *extensions.Registry, deps func() IcalDeps) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ParseIcsID, Source: source, Order: 490}, Title: "Parse iCalendar", Category: "Time",
		Description: "Reads an iCalendar (.ics) file or text, such as an invitation attached to a mail or a subscribed feed, into its method (REQUEST, REPLY, CANCEL, PUBLISH …), its events " + eventDesc + " and its free/busy [{who, start, end, busy: [{start, end, type}]}]. Time zones are mapped to IANA ones (Windows names as Outlook writes them too); an exception of a series has recurrenceId and seriesId. The file is parsed in a limited child process, never in the server, at most 1 MB. It never fails the run: OK is false and Error says why in plain words, and Problems lists what was skipped.",
		Example:     "Parse iCalendar of a mail's text/calendar attachment, then show the Invitation card with Accept, Tentative and Decline",
		Tags:        []string{"icalendar", "ics", "invitation", "itip", "parse", "calendar"},
		Kinds:       []string{livenodes.KindAction, livenodes.KindTrigger, uinodes.KindHandler, cognition.KindFunction, cognition.KindAgent, livenodes.KindConnectorOp},
		Sample:      cognition.Values{"method": "REQUEST", "events": []any{}, "freeBusy": []any{}, "problems": []any{}, "ok": true, "error": ""},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				{ID: "file", Name: "File", Type: "json", Optional: true, Description: "An uploaded file as a file field holds it, {id, name, size, contentType}, or its id; read in place of Text"},
				{ID: "text", Name: "Text", Type: "string", Optional: true, Description: "The .ics text, read when no file is given"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("method", "Method", "string", "The file's METHOD, upper case; empty when it has none"),
				cognition.DataPin("events", "Events", "json", "The events in the file's order"),
				cognition.DataPin("freeBusy", "Free/busy", "json", "[{who, start, end, busy: [{start, end, type}]}]"),
				cognition.DataPin("problems", "Problems", "json", "What could not be read and was skipped or guessed"),
				cognition.DataPin("ok", "OK", "bool", "Whether the file was read"),
				cognition.DataPin("error", "Error", "string", "Why it was not read; empty when OK"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			fail := func(msg string) (cognition.Values, string, error) {
				return cognition.Values{"method": "", "events": []any{}, "freeBusy": []any{}, "problems": []any{}, "ok": false, "error": msg}, cognition.Then, nil
			}
			var d IcalDeps
			if deps != nil {
				d = deps()
			}
			var fileID string
			switch f := in["file"].(type) {
			case string:
				fileID = f
			case map[string]any:
				fileID, _ = f["id"].(string)
			}
			var raw []byte
			switch {
			case fileID != "":
				if d.Open == nil {
					return fail("files cannot be read here")
				}
				rc, _, found, err := d.Open(x.Context(), x.Principal.TenantID, fileID)
				if err != nil {
					return fail("the file could not be opened")
				}
				if !found {
					return fail("the file is missing: it may have been deleted")
				}
				raw, err = io.ReadAll(io.LimitReader(rc, ical.MaxBytes+1))
				rc.Close()
				if err != nil {
					return fail("the file could not be read")
				}
			case str(in, "text") != "":
				raw = []byte(str(in, "text"))
			default:
				return fail("no file or text was given")
			}
			if len(raw) > ical.MaxBytes {
				return fail(ical.ErrTooLarge.Error())
			}
			if d.Read == nil {
				return fail("calendar files cannot be read here")
			}
			p, err := d.Read(x.Context(), raw)
			if err != nil {
				return fail(err.Error())
			}
			events, err := plainList(p.Events)
			if err != nil {
				return nil, "", err
			}
			fb, err := plainList(p.FreeBusy)
			if err != nil {
				return nil, "", err
			}
			problems, _ := plainList(p.Problems)
			return cognition.Values{"method": p.Method, "events": events, "freeBusy": fb, "problems": problems, "ok": true, "error": ""}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ComposeIcsID, Source: source, Order: 491}, Title: "Compose iCalendar", Category: "Time",
		Description: "Writes events as an iCalendar (.ics) text with a method: PUBLISH (the default), REQUEST (an invitation), REPLY, CANCEL, COUNTER or DECLINECOUNTER. Events are " + eventDesc + "; each needs an iCalUID or id. The text carries a VTIMEZONE for each zone it uses, read from the server's zone database, folded lines and escaped text, ready to attach to a mail as text/calendar.",
		Example:     "Compose iCalendar of the confirmed meeting with method REQUEST, then attach it to the invitation mail",
		Tags:        []string{"icalendar", "ics", "invitation", "itip", "compose", "calendar"},
		Config:      json.RawMessage(`{"type":"object","properties":{"method":{"type":"string","title":"Method","enum":["PUBLISH","REQUEST","REPLY","CANCEL","COUNTER","DECLINECOUNTER"],"description":"The iTIP method; the Method pin wins"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "events", Name: "Events", Type: "json", Description: "The events to write"},
				{ID: "method", Name: "Method", Type: "string", Optional: true, Description: "The method, in place of the config's"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("text", "Text", "string", "The .ics text"),
				cognition.DataPin("contentType", "Content type", "string", "text/calendar; charset=utf-8; method=<method>, for the attachment"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Method string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if m := str(in, "method"); m != "" {
				c.Method = m
			}
			var events []calendar.Event
			if err := decode(in["events"], &events); err != nil {
				return nil, "", fmt.Errorf("events: %v", err)
			}
			text, err := ical.Compose(c.Method, events, ical.ComposeOptions{})
			if err != nil {
				return nil, "", err
			}
			m := strings.ToUpper(c.Method)
			if m == "" {
				m = ical.MethodPublish
			}
			return cognition.Values{"text": text, "contentType": "text/calendar; charset=utf-8; method=" + m}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ReplyID, Source: source, Order: 492}, Title: "iTIP reply", Category: "Time",
		Description: "The answer an invited person sends back to the organizer: the invitation (an event from Parse iCalendar), the attendee's email and their answer (accepted, tentative or declined, with an optional comment) give the REPLY event and its .ics text (method REPLY), to mail to the organizer.",
		Example:     "iTIP reply to the parsed invitation as the person who pressed Accept, then send its text to the organizer",
		Tags:        []string{"icalendar", "itip", "reply", "rsvp", "invitation", "calendar"},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "invitation", Name: "Invitation", Type: "json", Description: "The invitation's event"},
				{ID: "attendee", Name: "Attendee", Type: "string", Description: "The answering person's email"},
				{ID: "answer", Name: "Answer", Type: "string", Description: "accepted, tentative or declined"},
				{ID: "comment", Name: "Comment", Type: "string", Optional: true, Description: "A note to the organizer"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("reply", "Reply", "json", "The REPLY event"),
				cognition.DataPin("text", "Text", "string", "The REPLY's .ics text"),
				cognition.DataPin("to", "To", "string", "The organizer's email"),
			},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var inv calendar.Event
			if err := decode(in["invitation"], &inv); err != nil {
				return nil, "", fmt.Errorf("invitation: %v", err)
			}
			r, err := ical.Reply(inv, str(in, "attendee"), str(in, "answer"), str(in, "comment"))
			if err != nil {
				return nil, "", err
			}
			text, err := ical.Compose(ical.MethodReply, []calendar.Event{r}, ical.ComposeOptions{})
			if err != nil {
				return nil, "", err
			}
			rv, err := plain(r)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"reply": rv, "text": text, "to": r.Organizer.Email}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ApplyID, Source: source, Order: 493}, Title: "iTIP apply", Category: "Time",
		Description: "Applies an incoming iTIP message to the event it is about, whatever carried it (a mail, an adapter): a REPLY sets that attendee's answer, a REQUEST or PUBLISH replaces the details (keeping answers given at the same SEQUENCE), a CANCEL cancels it. Refused, with Applied false and Reason, when the message is about another event or occurrence (other), older than the event (older: a lower SEQUENCE), a reply from someone not invited (notInvited), a bad answer (badResponse) or a COUNTER, which a person decides (notApplied).",
		Example:     "iTIP apply a REPLY parsed from a mail to the organizer's event, then Update record with the result when Applied",
		Tags:        []string{"icalendar", "itip", "apply", "reply", "invitation", "calendar"},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "event", Name: "Event", Type: "json", Description: "The existing event"},
				{ID: "method", Name: "Method", Type: "string", Description: "The message's method"},
				{ID: "message", Name: "Message", Type: "json", Description: "The message's event"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("event", "Event", "json", "The updated event; the existing one when refused"),
				cognition.DataPin("applied", "Applied", "bool", "Whether it was applied"),
				cognition.DataPin("reason", "Reason", "string", "other, older, notInvited, badResponse or notApplied when refused"),
				cognition.DataPin("error", "Error", "string", "The refusal in plain words"),
			},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var ev, msg calendar.Event
			if err := decode(in["event"], &ev); err != nil {
				return nil, "", fmt.Errorf("event: %v", err)
			}
			if err := decode(in["message"], &msg); err != nil {
				return nil, "", fmt.Errorf("message: %v", err)
			}
			out, err := ical.Apply(ev, str(in, "method"), msg)
			reason, text := "", ""
			if err != nil {
				text = err.Error()
				switch {
				case errors.Is(err, ical.ErrOtherEvent):
					reason = "other"
				case errors.Is(err, ical.ErrOlder):
					reason = "older"
				case errors.Is(err, ical.ErrNotInvited):
					reason = "notInvited"
				case errors.Is(err, ical.ErrBadResponse):
					reason = "badResponse"
				default:
					reason = "notApplied"
				}
			}
			ov, perr := plain(out)
			if perr != nil {
				return nil, "", perr
			}
			return cognition.Values{"event": ov, "applied": err == nil, "reason": reason, "error": text}, "", nil
		},
	})
}
