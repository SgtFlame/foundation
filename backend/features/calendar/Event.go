// Package calendar is the calendar's contract and shapes (forge 28): the
// event shape every calendar connector type speaks (calendar.events@1),
// its free/busy shape, and the builtin contract. Date maths is in
// calendar/when, the iCalendar codec in calendar/ical, the Calendar test
// server in calendar/caltest and the contract suite in
// calendar/contracttest.
package calendar

import "github.com/SgtFlame/foundation/backend/features/calendar/when"

// Event statuses.
const (
	StatusConfirmed = "confirmed"
	StatusTentative = "tentative"
	StatusCancelled = "cancelled"
)

// Transparency: whether an event makes its time busy.
const (
	Opaque      = "opaque"
	Transparent = "transparent"
)

// Attendee responses.
const (
	NeedsAction = "needsAction"
	Accepted    = "accepted"
	Tentative   = "tentative"
	Declined    = "declined"
	Delegated   = "delegated"
)

// Person is an organizer or attendee.
type Person struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Attendee is an invited person and their answer.
type Attendee struct {
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	Response  string `json:"response,omitempty"` // needsAction, accepted, tentative, declined, delegated
	Optional  bool   `json:"optional,omitempty"`
	Organizer bool   `json:"organizer,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

// Event is an event as calendar.events@1 carries it: a one-off, a
// recurring series' master (with Recurrence) or an exception of one (with
// RecurrenceID and SeriesID). Start and End are zoned times or all-day
// dates (End exclusive).
type Event struct {
	ID              string           `json:"id,omitempty"`
	Etag            string           `json:"etag,omitempty"`
	Calendar        string           `json:"calendar,omitempty"`
	Title           string           `json:"title"`
	Start           when.Zoned       `json:"start"`
	End             when.Zoned       `json:"end"`
	Recurrence      *when.Recurrence `json:"recurrence,omitempty"`
	RecurrenceID    string           `json:"recurrenceId,omitempty"`
	SeriesID        string           `json:"seriesId,omitempty"`
	Status          string           `json:"status,omitempty"`
	Transparency    string           `json:"transparency,omitempty"`
	Location        string           `json:"location,omitempty"`
	VideoLink       string           `json:"videoLink,omitempty"`
	URL             string           `json:"url,omitempty"`
	Organizer       *Person          `json:"organizer,omitempty"`
	Attendees       []Attendee       `json:"attendees,omitempty"`
	Description     string           `json:"description,omitempty"`
	DescriptionHTML string           `json:"descriptionHtml,omitempty"`
	ICalUID         string           `json:"iCalUID,omitempty"`
	Sequence        int              `json:"sequence,omitempty"`
	Updated         string           `json:"updated,omitempty"`
}

// AllDay tells whether e is an all-day event.
func (e Event) AllDay() bool { return e.Start.AllDay() }

// Busy is one busy range of a free/busy answer.
type Busy struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Type  string `json:"type,omitempty"` // busy, tentative, unavailable
}

// FreeBusy is a person's or calendar's busy time in a window.
type FreeBusy struct {
	Who   string `json:"who"` // a calendar id or an email
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	Busy  []Busy `json:"busy"`
	Error string `json:"error,omitempty"` // why nothing is known, e.g. no calendar for this person
}

// Calendar is a calendar as calendar.list gives it.
type Calendar struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color,omitempty"`
	Zone    string `json:"zone,omitempty"`
	Access  string `json:"access,omitempty"` // owner, writer, reader, freeBusy
	Primary bool   `json:"primary,omitempty"`
}
