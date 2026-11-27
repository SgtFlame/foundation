package ical

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/calendar"
)

// Refusals of Apply, as codes a graph can branch on.
var (
	ErrOtherEvent  = errors.New("the message is about another event")
	ErrOlder       = errors.New("the message is older than the event (its SEQUENCE is lower)")
	ErrNotInvited  = errors.New("the reply is from someone who is not invited")
	ErrNotApplied  = errors.New("this kind of message is not applied automatically")
	ErrBadResponse = errors.New("the answer must be accepted, tentative or declined")
)

// Reply is the REPLY event an attendee sends back to the organizer: the
// invitation's UID, SEQUENCE, recurrence id and times, its organizer, and
// the attendee alone with their answer (accepted, tentative or declined).
// Compose it with MethodReply.
func Reply(invitation calendar.Event, attendee string, answer string, comment string) (calendar.Event, error) {
	answer = normalAnswer(answer)
	if answer == "" {
		return calendar.Event{}, ErrBadResponse
	}
	attendee = strings.ToLower(strings.TrimSpace(attendee))
	if attendee == "" {
		return calendar.Event{}, errors.New("give the attendee's email")
	}
	if invitation.Organizer == nil || invitation.Organizer.Email == "" {
		return calendar.Event{}, errors.New("the invitation has no organizer to reply to")
	}
	r := calendar.Event{ID: invitation.ID, ICalUID: invitation.ICalUID, Title: invitation.Title, Start: invitation.Start, End: invitation.End,
		RecurrenceID: invitation.RecurrenceID, Sequence: invitation.Sequence, Organizer: invitation.Organizer}
	a := calendar.Attendee{Email: attendee, Response: answer, Comment: comment}
	for _, x := range invitation.Attendees {
		if strings.EqualFold(x.Email, attendee) {
			a.Name, a.Optional = x.Name, x.Optional
		}
	}
	r.Attendees = []calendar.Attendee{a}
	return r, nil
}

func normalAnswer(a string) string {
	switch strings.ToLower(strings.ReplaceAll(a, "-", "")) {
	case "accepted", "accept", "yes":
		return calendar.Accepted
	case "tentative", "maybe":
		return calendar.Tentative
	case "declined", "decline", "no":
		return calendar.Declined
	}
	return ""
}

// Apply applies an incoming iTIP message (its method and one of its
// events) to the event it is about, giving the updated event or a
// refusal: a message about another UID or occurrence, an older SEQUENCE,
// a reply from someone not invited, or a COUNTER (which a person
// decides). A REQUEST or PUBLISH replaces the event's details, keeping
// answers already given at the same SEQUENCE; a REPLY sets one attendee's
// answer; a CANCEL cancels it.
func Apply(existing calendar.Event, method string, msg calendar.Event) (calendar.Event, error) {
	uid := func(e calendar.Event) string {
		if e.ICalUID != "" {
			return e.ICalUID
		}
		return e.ID
	}
	if uid(existing) != uid(msg) || existing.RecurrenceID != msg.RecurrenceID {
		return existing, ErrOtherEvent
	}
	if msg.Sequence < existing.Sequence {
		return existing, ErrOlder
	}
	out := existing
	switch strings.ToUpper(method) {
	case MethodReply:
		if len(msg.Attendees) == 0 {
			return existing, ErrNotInvited
		}
		from := msg.Attendees[0]
		answer := normalAnswer(from.Response)
		if answer == "" {
			return existing, ErrBadResponse
		}
		found := false
		out.Attendees = append([]calendar.Attendee(nil), existing.Attendees...)
		for i := range out.Attendees {
			if strings.EqualFold(out.Attendees[i].Email, from.Email) {
				out.Attendees[i].Response = answer
				out.Attendees[i].Comment = from.Comment
				found = true
			}
		}
		if !found {
			return existing, fmt.Errorf("%w: %s", ErrNotInvited, from.Email)
		}
	case MethodCancel:
		out.Status = calendar.StatusCancelled
		out.Sequence = msg.Sequence
	case MethodRequest, MethodPublish, "":
		keep := map[string]string{}
		if msg.Sequence == existing.Sequence {
			for _, a := range existing.Attendees {
				if a.Response != "" && a.Response != calendar.NeedsAction {
					keep[strings.ToLower(a.Email)] = a.Response
				}
			}
		}
		id, etag, cal := existing.ID, existing.Etag, existing.Calendar
		out = msg
		out.ID, out.Etag, out.Calendar = id, etag, cal
		out.Attendees = append([]calendar.Attendee(nil), msg.Attendees...)
		for i, a := range out.Attendees {
			if r, ok := keep[strings.ToLower(a.Email)]; ok && (a.Response == "" || a.Response == calendar.NeedsAction) {
				out.Attendees[i].Response = r
			}
		}
	default:
		return existing, ErrNotApplied
	}
	return out, nil
}
