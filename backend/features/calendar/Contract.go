package calendar

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// EventsRef is the calendar contract's reference (forge 28c): one contract
// for outside calendars, so the Calendar app's sync and tools are the same
// for Google, Microsoft, CalDAV and an ICS feed.
const (
	EventsID  = "calendar.events"
	EventsRef = "calendar.events@1"
)

// CodeCursorExpired is the code an expired sync cursor's invalid error
// carries in its message: the caller lists the window again.
const CodeCursorExpired = "cursor_expired"

const source = "calendar"

const zonedSchema = `{"type":"object","description":"A zoned time {local, zone} or an all-day date {date}","properties":{
  "local":{"type":"string","description":"The local time without an offset, 2026-03-29T09:00:00"},
  "zone":{"type":"string","description":"The IANA zone, e.g. Europe/London"},
  "date":{"type":"string","description":"An all-day date, 2026-03-29; the end date is exclusive"}}}`

// EventSchema is the event shape's JSON Schema.
const EventSchema = `{"type":"object","description":"An event: a one-off, a recurring series' master (with recurrence) or an exception of one (with recurrenceId and seriesId)","properties":{
  "id":{"type":"string","description":"The event's id at the service"},
  "etag":{"type":"string","description":"Its version at the service; an update names the etag it was based on"},
  "calendar":{"type":"string","description":"The calendar's id"},
  "title":{"type":"string","description":"Its title"},
  "start":` + zonedSchema + `,
  "end":` + zonedSchema + `,
  "recurrence":{"type":"object","description":"A series' rule","properties":{
    "rrule":{"type":"string","description":"RFC 5545 RRULE, e.g. FREQ=WEEKLY;BYDAY=TU"},
    "rdate":{"type":"array","items":{"type":"string"},"description":"Extra starts"},
    "exdate":{"type":"array","items":{"type":"string"},"description":"Starts left out"}}},
  "recurrenceId":{"type":"string","description":"For an exception: the start its occurrence would have had (an instant, or a date)"},
  "seriesId":{"type":"string","description":"For an exception: its series' id"},
  "status":{"type":"string","enum":["confirmed","tentative","cancelled"],"description":"Its status"},
  "transparency":{"type":"string","enum":["opaque","transparent"],"description":"opaque makes its time busy, transparent leaves it free"},
  "location":{"type":"string","description":"Where"},
  "videoLink":{"type":"string","description":"The video call's link"},
  "url":{"type":"string","description":"A link to the event"},
  "organizer":{"type":"object","description":"Who organizes it","properties":{"email":{"type":"string","description":"Email"},"name":{"type":"string","description":"Name"}}},
  "attendees":{"type":"array","description":"Who is invited and their answers","items":{"type":"object","properties":{
    "email":{"type":"string","description":"Email"},
    "name":{"type":"string","description":"Name"},
    "response":{"type":"string","enum":["needsAction","accepted","tentative","declined","delegated"],"description":"Their answer"},
    "optional":{"type":"boolean","description":"Whether they are optional"},
    "organizer":{"type":"boolean","description":"Whether this is the organizer"},
    "comment":{"type":"string","description":"Their note"}},"required":["email"]}},
  "description":{"type":"string","description":"Its description as plain text"},
  "descriptionHtml":{"type":"string","description":"Its description as HTML, when the service has one"},
  "iCalUID":{"type":"string","description":"Its iCalendar UID, the same at every service"},
  "sequence":{"type":"integer","description":"Its iCalendar SEQUENCE"},
  "updated":{"type":"string","description":"When it last changed, RFC 3339"}},"required":["title","start","end"]}`

// EventExample is an example event.
const EventExample = `{"id":"evt-1","etag":"1","calendar":"primary","title":"Renewal call","start":{"local":"2026-12-03T10:00:00","zone":"America/New_York"},"end":{"local":"2026-12-03T11:00:00","zone":"America/New_York"},"status":"confirmed","transparency":"opaque","organizer":{"email":"tony@example.org"},"attendees":[{"email":"dana@example.com","response":"needsAction"}],"iCalUID":"evt-1@axiom"}`

const windowSchema = `{"type":"object","description":"A range of instants, to exclusive","properties":{"from":{"type":"string","description":"RFC 3339"},"to":{"type":"string","description":"RFC 3339"}},"required":["from","to"]}`

func obj(props string, required ...string) json.RawMessage {
	r := ""
	if len(required) > 0 {
		b, _ := json.Marshal(required)
		r = `,"required":` + string(b)
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `}` + r + `}`)
}

// EventsContract is calendar.events@1.
func EventsContract() connectors.Contract {
	retryable := []string{connectors.CodeRateLimited, connectors.CodeUnavailable}
	calendarSchema := `{"type":"object","properties":{
  "id":{"type":"string","description":"The calendar's id"},
  "name":{"type":"string","description":"Its name"},
  "color":{"type":"string","description":"Its colour, #rrggbb"},
  "zone":{"type":"string","description":"Its time zone"},
  "access":{"type":"string","enum":["owner","writer","reader","freeBusy"],"description":"What this connection may do with it"},
  "primary":{"type":"boolean","description":"Whether it is the account's main calendar"}},"required":["id","name"]}`
	eventResult := json.RawMessage(EventSchema)
	ok := obj(`"ok":{"type":"boolean","description":"Always true: a failure is an error"}`)
	return connectors.Contract{
		ID: EventsID, Version: 1,
		Meta: connectors.Meta{Title: "Calendar events", Description: "Reads and changes the events of an outside calendar service (Google, Microsoft 365, CalDAV, a subscribed feed): its calendars, events in a window or the changes since a sync cursor, creating (keyed by our id, so a retry makes one), updating with the etag it was based on, deleting, answering invitations and free/busy. Every calendar type implements it, so the Calendar app's sync and tools are the same for every service.", Tags: []string{"calendar", "events", "scheduling", "sync"}},
		Operations: []connectors.Operation{
			{
				ID: "calendar.list", Title: "List calendars", Description: "The calendars this connection can see, each with its name, colour, zone, what the connection may do with it and whether it is the account's primary one.",
				Effect: connectors.EffectRead, Args: obj(``),
				Result:   obj(`"calendars":{"type":"array","items":`+calendarSchema+`,"description":"The calendars"}`, "calendars"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"calendars":[{"id":"primary","name":"Tony","color":"#3f51b5","zone":"Europe/London","access":"owner","primary":true}]}`)}},
				Errors:   append([]string{connectors.CodeAuth}, retryable...),
			},
			{
				ID: "event.list", Title: "List events", Description: "The events of a calendar overlapping a window (a recurring series once, as its master with its rule, and its exceptions), or with a cursor the changes since it: changed events, and removed ones as {id, removed: true}. Either way it gives the next cursor to store. A cursor too old is refused as invalid with cursor_expired in its message: list the window again without one.",
				Effect: connectors.EffectRead,
				Args: obj(`"calendar":{"type":"string","description":"The calendar's id"},
  "window":`+windowSchema+`,
  "cursor":{"type":"string","description":"The cursor from the last list, to get only changes since"}`, "calendar"),
				Result: obj(`"events":{"type":"array","items":`+EventSchema+`,"description":"The events, or the changed ones with a cursor"},
  "removed":{"type":"array","items":{"type":"string"},"description":"Ids of events removed since the cursor"},
  "cursor":{"type":"string","description":"The cursor to store for the next list"}`, "events", "cursor"),
				Examples: []connectors.Example{{Title: "A week", Args: json.RawMessage(`{"calendar":"primary","window":{"from":"2026-11-30T00:00:00Z","to":"2026-12-07T00:00:00Z"}}`), Result: json.RawMessage(`{"events":[` + EventExample + `],"removed":[],"cursor":"7"}`)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeInvalid, connectors.CodeAuth}, retryable...),
			},
			{
				ID: "event.get", Title: "Get an event", Description: "One event by its id, with its etag. notFound when it is gone.",
				Effect:   connectors.EffectRead,
				Args:     obj(`"calendar":{"type":"string","description":"The calendar's id"},"id":{"type":"string","description":"The event's id"}`, "calendar", "id"),
				Result:   eventResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"calendar":"primary","id":"evt-1"}`), Result: json.RawMessage(EventExample)}},
				Errors:   append([]string{connectors.CodeNotFound}, retryable...),
			},
			{
				ID: "event.create", Title: "Create an event", Description: "Makes an event under our id (the key: a create with an id already made makes nothing and gives that event), and gives it with the service's id and etag. A series is made by giving recurrence.",
				Effect: connectors.EffectWrite, Key: "key",
				Args: obj(`"calendar":{"type":"string","description":"The calendar's id"},
  "key":{"type":"string","description":"Our id for the event, the idempotency key"},
  "event":`+EventSchema, "calendar", "key", "event"),
				Result:   eventResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"calendar":"primary","key":"rec-123","event":{"title":"Renewal call","start":{"local":"2026-12-03T10:00:00","zone":"America/New_York"},"end":{"local":"2026-12-03T11:00:00","zone":"America/New_York"}}}`), Result: json.RawMessage(EventExample)}},
				Errors:   append([]string{connectors.CodeInvalid, connectors.CodeNotFound, connectors.CodeAuth}, retryable...),
			},
			{
				ID: "event.update", Title: "Update an event", Description: "Changes the fields of set on an event, refused as conflict when its etag moved since the version the change was based on (someone changed it at the service): read it again and decide. Gives the event with its new etag.",
				Effect: connectors.EffectWrite,
				Args: obj(`"calendar":{"type":"string","description":"The calendar's id"},
  "id":{"type":"string","description":"The event's id"},
  "etag":{"type":"string","description":"The etag the change was based on; left out, the change is made whatever"},
  "set":{"type":"object","description":"The fields to change, in the event shape"}`, "calendar", "id", "set"),
				Result:   eventResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"calendar":"primary","id":"evt-1","etag":"1","set":{"title":"Renewal call (moved)"}}`), Result: json.RawMessage(EventExample)}},
				Errors:   append([]string{connectors.CodeConflict, connectors.CodeNotFound, connectors.CodeInvalid}, retryable...),
			},
			{
				ID: "event.delete", Title: "Delete an event", Description: "Deletes an event (a whole series when it is a master). Deleting one that is gone succeeds, with deleted false.",
				Effect: connectors.EffectWrite, Destructive: true,
				Args:     obj(`"calendar":{"type":"string","description":"The calendar's id"},"id":{"type":"string","description":"The event's id"},"etag":{"type":"string","description":"The etag the delete was based on; refused as conflict when it moved"}`, "calendar", "id"),
				Result:   obj(`"deleted":{"type":"boolean","description":"Whether there was an event to delete"}`, "deleted"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"calendar":"primary","id":"evt-1"}`), Result: json.RawMessage(`{"deleted":true}`)}},
				Errors:   append([]string{connectors.CodeConflict}, retryable...),
			},
			{
				ID: "event.respond", Title: "Answer an invitation", Description: "Answers an invitation on this calendar as the connection's own account: accepted, tentative or declined, with an optional comment. Only the account's own attendee entry changes; the service tells the organizer.",
				Effect: connectors.EffectWrite,
				Args: obj(`"calendar":{"type":"string","description":"The calendar's id"},
  "id":{"type":"string","description":"The event's id"},
  "answer":{"type":"string","enum":["accepted","tentative","declined"],"description":"The answer"},
  "comment":{"type":"string","description":"A note to the organizer"}`, "calendar", "id", "answer"),
				Result:   eventResult,
				Examples: []connectors.Example{{Args: json.RawMessage(`{"calendar":"primary","id":"evt-1","answer":"accepted"}`), Result: json.RawMessage(EventExample)}},
				Errors:   append([]string{connectors.CodeNotFound, connectors.CodeInvalid}, retryable...),
			},
			{
				ID: "freeBusy", Title: "Free/busy", Description: "Busy time in a window for calendars of this connection or for people the service can see, each {who, busy: [{start, end, type}]} or an error when nothing is known about them.",
				Effect: connectors.EffectRead,
				Args: obj(`"calendars":{"type":"array","items":{"type":"string"},"description":"Calendar ids"},
  "people":{"type":"array","items":{"type":"string"},"description":"Emails of people the service may know"},
  "window":`+windowSchema, "window"),
				Result: obj(`"freeBusy":{"type":"array","description":"One per calendar or person","items":{"type":"object","properties":{
    "who":{"type":"string","description":"The calendar id or email"},
    "busy":{"type":"array","description":"Busy ranges","items":{"type":"object","properties":{"start":{"type":"string","description":"RFC 3339"},"end":{"type":"string","description":"RFC 3339"},"type":{"type":"string","description":"busy, tentative or unavailable"}},"required":["start","end"]}},
    "error":{"type":"string","description":"Why nothing is known"}},"required":["who","busy"]}}`, "freeBusy"),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"calendars":["primary"],"window":{"from":"2026-11-30T00:00:00Z","to":"2026-12-07T00:00:00Z"}}`), Result: json.RawMessage(`{"freeBusy":[{"who":"primary","busy":[{"start":"2026-12-03T15:00:00Z","end":"2026-12-03T16:00:00Z","type":"busy"}]}]}`)}},
				Errors:   retryable,
			},
			{
				ID: "check", Title: "Check", Description: "Signs in and reads the calendar list: whether the connection's credentials work.",
				Effect: connectors.EffectRead, Args: obj(``), Result: ok,
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeUnavailable},
			},
		},
		Events: []connectors.EventDef{
			{
				ID: "eventChanged", Title: "Event changed", Description: "An event was made or changed at the service. Sync by listing changes with the stored cursor rather than trusting the payload or the order of events.",
				Payload:  obj(`"id":{"type":"string","description":"The notice's own id, unique"},"calendar":{"type":"string","description":"The calendar's id"},"event":{"type":"string","description":"The event's id"},"etag":{"type":"string","description":"Its new etag"}`, "id", "calendar"),
				Example:  json.RawMessage(`{"id":"n-1","calendar":"primary","event":"evt-1","etag":"2"}`),
				IDFields: []string{"id"}, Delivery: []string{connectors.DeliveryWatch, connectors.DeliveryCallback}, Subscription: "eventChanged",
			},
			{
				ID: "eventRemoved", Title: "Event removed", Description: "An event was deleted at the service.",
				Payload:  obj(`"id":{"type":"string","description":"The notice's own id, unique"},"calendar":{"type":"string","description":"The calendar's id"},"event":{"type":"string","description":"The event's id"}`, "id", "calendar"),
				Example:  json.RawMessage(`{"id":"n-2","calendar":"primary","event":"evt-1"}`),
				IDFields: []string{"id"}, Delivery: []string{connectors.DeliveryWatch, connectors.DeliveryCallback}, Subscription: "eventRemoved",
			},
			{
				ID: "calendarsChanged", Title: "Calendars changed", Description: "A calendar was added, renamed or removed: list the calendars again.",
				Payload:  obj(`"id":{"type":"string","description":"The notice's own id, unique"}`, "id"),
				Example:  json.RawMessage(`{"id":"n-3"}`),
				IDFields: []string{"id"}, Delivery: []string{connectors.DeliveryWatch, connectors.DeliveryCallback}, Subscription: "calendarsChanged",
			},
		},
	}
}

// Contribute registers the calendar contract.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, source, []connectors.Contract{EventsContract()}, nil)
}
