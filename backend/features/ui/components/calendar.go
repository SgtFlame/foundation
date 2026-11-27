package uicomponents

// mui.calendar (forge 28d) is kept in its own file, appended to specs, so
// parallel work on other primitives does not touch the same lines.
func init() {
	specs = append(specs, spec{id: "mui.calendar", title: "Calendar", category: "Collection",
		tags: []string{"calendar", "schedule", "events", "month", "week", "day", "agenda", "dates", "drag"},
		description: "Dated items in a month, week, work week, day or agenda layout (forge 28d): items that overlap share their column, all-day and multi-day items sit in a strip above the grid, a month cell shows +n more, and the current-time line shows on today. " +
			"Generic: an item is {id, title, start, end, allDay, color, readOnly, group}, with start and end as RFC 3339 instants (or dates for all-day items), drawn in zone (the viewer's when left out). " +
			"With editable, an item dragged or its end stretched is snapped to slotMinutes and sends one move or resize on drop; it stays where it was dropped until the server's value changes. With selectable, dragging over empty slots sends select (to create). " +
			"Its toolbar (today, back, forward, the view) sends view and range; bind the page's query to the range it sends. With groups, the day view shows a column per group (people or rooms) and items go to their group's column.",
		props: `"items":{"type":"array","description":"The items: {id, title, start, end, allDay, color, readOnly, group}","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"start":{"type":"string","description":"An RFC 3339 instant, or YYYY-MM-DD when allDay"},"end":{"type":"string","description":"Exclusive; an all-day item's is the day after its last"},"allDay":{"type":"boolean"},"color":{"type":"string","description":"A theme palette name (primary, secondary, success, warning, error, info) or a CSS colour"},"readOnly":{"type":"boolean"},"group":{"type":"string"}}}},` +
			`"view":{"type":"string","enum":["month","week","workWeek","day","agenda"],"description":"The layout, week when left out"},` +
			`"date":{"type":"string","description":"The focused day, YYYY-MM-DD; today when left out"},` +
			`"zone":{"type":"string","description":"The IANA time zone to draw in; the viewer's when left out"},` +
			`"workingHours":{"type":"object","description":"{start: \"09:00\", end: \"17:00\"}, shaded as working time and scrolled to"},` +
			`"slotMinutes":{"type":"integer","minimum":5,"description":"The grid's step and drag snap, 15 when left out"},` +
			`"groups":{"type":"array","description":"Day view columns side by side: {id, title}","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"}}}},` +
			`"selectable":{"type":"boolean","description":"Dragging over empty slots sends select"},` +
			`"editable":{"type":"boolean","description":"Items can be dragged and resized"},` +
			`"showNow":{"type":"boolean","description":"The current-time line, true when left out"},` +
			`"toolbar":{"type":"boolean","description":"Today, back, forward and the view switch, true when left out"},` +
			`"height":{"type":"number","description":"Pixels; fills its box when left out"},` +
			`"busy":{"type":"object","description":"itemId → [{name}] of others on that item (presence), shown as a ring"},` + sx,
		events: ev("move", "An item was dragged to a new time: payload {id, start, end} (and group when dropped in another group's column)",
			"resize", "An item's end was stretched: payload {id, end}",
			"select", "Empty slots were dragged over: payload {start, end, allDay} (and group)",
			"open", "An item was clicked or Enter pressed on it: payload {id}",
			"range", "The visible range changed (on first draw too): payload {from, to, view, date}",
			"view", "The toolbar changed the view or the date: payload {view, date}"),
		example: `{"type":"mui.calendar","key":"cal","props":{"view":"week","date":"2026-12-01","editable":true,"selectable":true,"items":[{"id":"e1","title":"Standup","start":"2026-12-01T09:00:00Z","end":"2026-12-01T09:15:00Z"}]},"on":{"move":{"handler":"onMove","payload":{"id":"event.id","start":"event.start","end":"event.end"}}}}`,
	})
}
