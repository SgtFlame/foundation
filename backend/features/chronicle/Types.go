package chronicle

import (
	"encoding/json"
	"slices"
)

// EntryType is a kind of entry. The built-in types are code until custom
// types arrive with Cognition's summarizer graphs (Cognition.md); Fields is
// the schema of the type's own fields, in the config schema language, so
// the entry editor renders them with the Settings form.
type EntryType struct {
	ID          string
	Title       string
	Icon        string // an emoji
	Description string // what the memory update reads when it proposes an entry of this type
	Fields      json.RawMessage
}

// Types are the built-in entry types, in the order the Chronicles view
// groups them.
var Types = []EntryType{
	{ID: "entity", Title: "Entities", Icon: "👤", Description: "a person, place, organisation or thing",
		Fields: json.RawMessage(`{"type": "object", "properties": {"kind": {"type": "enum", "title": "Kind", "options": [
			{"value": "person", "label": "Person"}, {"value": "place", "label": "Place"},
			{"value": "organisation", "label": "Organisation"}, {"value": "thing", "label": "Thing"}]}}}`)},
	{ID: "note", Title: "Notes", Icon: "📝", Description: "general-purpose notes on a topic, in markdown",
		Fields: json.RawMessage(`{"type": "object", "properties": {}}`)},
	{ID: "wiki", Title: "Wikis", Icon: "📚", Description: "a wiki page that gathers what is known about a subject, linking related entries",
		Fields: json.RawMessage(`{"type": "object", "properties": {}}`)},
	{ID: "timeline", Title: "Timelines", Icon: "🕒", Description: "a sequence of events, whose events are its children",
		Fields: json.RawMessage(`{"type": "object", "properties": {"kind": {"type": "enum", "title": "Kind", "options": [
			{"value": "concrete", "label": "Dated events"}, {"value": "ordinal", "label": "Ordered events (no dates)"}]}}}`)},
	{ID: "event", Title: "Events", Icon: "📍", Description: "something that happened or will happen, usually the child of a timeline",
		Fields: json.RawMessage(`{"type": "object", "properties": {
			"date": {"type": "string", "title": "Date", "description": "When it happened, e.g. 2026-10-23 or 1815"},
			"order": {"type": "number", "title": "Order", "description": "Its place in an ordinal timeline"}}}`)},
}

// TypeOf is the built-in type with id.
func TypeOf(id string) (EntryType, bool) {
	i := slices.IndexFunc(Types, func(t EntryType) bool { return t.ID == id })
	if i < 0 {
		return EntryType{}, false
	}
	return Types[i], true
}
