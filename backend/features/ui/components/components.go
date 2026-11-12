// Package uicomponents registers the primitive components: the MUI family,
// layout.flex (flexlayout sub-tabs) and graph.canvas (React Flow), each
// with the metadata a model reads (documentation/ServerUI.md › Primitives).
// The browser's renderers use the same ids.
package uicomponents

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

type spec struct {
	id, title, category, description string
	props                            string // the properties of the props schema
	required                         string // e.g. `"label"`
	events                           []ui.Event
	children                         bool
	slots                            []string
	example                          string
	tags                             []string
}

func ev(pairs ...string) []ui.Event {
	var out []ui.Event
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, ui.Event{Name: pairs[i], Description: pairs[i+1]})
	}
	return out
}

const (
	sx       = `"sx":{"type":"object","description":"MUI system styles, e.g. {\"p\": 2}"}`
	label    = `"label":{"type":"string","description":"The visible label"}`
	disabled = `"disabled":{"type":"boolean"}`
	grow     = `"grow":{"type":"boolean","description":"Take the rest of the row or column it is in, scrolling what does not fit (a frame's page beside its navigation)"}`
	value    = `"value":{"description":"The current value; bind it to local.<path> to keep typing in the browser"}`
	change   = "The value changed: payload {value}. Sent on every change, so bind value to local state and send it on submit instead where typing should not round-trip"
)

var specs = []spec{
	// Layout
	{id: "mui.stack", title: "Stack", category: "Layout", tags: []string{"layout", "row", "column"}, description: "Lays out its children in a row or a column with even spacing.",
		props:    `"direction":{"type":"string","enum":["row","column"]},"spacing":{"type":"number"},"alignItems":{"type":"string"},"justifyContent":{"type":"string"},"wrap":{"type":"boolean"},` + grow + `,"fill":{"type":"boolean","description":"Fill the height it is given, as a page's outermost stack does"},` + sx,
		children: true, example: `{"type":"mui.stack","key":"s","props":{"direction":"row","spacing":1},"children":[]}`},
	{id: "mui.grid", title: "Grid", category: "Layout", tags: []string{"layout", "columns", "responsive"}, description: "A responsive 12-column grid; each child's props.size (1-12) is its width.",
		props: `"spacing":{"type":"number"},"columns":{"type":"integer"},` + sx, children: true,
		example: `{"type":"mui.grid","key":"g","props":{"spacing":2},"children":[]}`},
	{id: "mui.box", title: "Box", category: "Layout", tags: []string{"container", "layout"}, description: "A plain container for spacing and styling its children.",
		props: grow + "," + sx, children: true, example: `{"type":"mui.box","key":"b","props":{"sx":{"p":2}}}`},
	{id: "mui.card", title: "Card", category: "Layout", tags: []string{"panel", "surface", "container"}, description: "A raised surface grouping related content, with an optional title and subtitle.",
		props:    `"title":{"type":"string"},"subtitle":{"type":"string"},"variant":{"type":"string","enum":["elevation","outlined"]},` + sx,
		children: true, slots: []string{"actions"}, example: `{"type":"mui.card","key":"c","props":{"title":"Counter"},"children":[]}`},
	{id: "mui.divider", title: "Divider", category: "Layout", tags: []string{"separator", "rule"}, description: "A thin line separating content.",
		props: `"orientation":{"type":"string","enum":["horizontal","vertical"]}`, example: `{"type":"mui.divider","key":"d"}`},
	// Display
	{id: "mui.text", title: "Text", category: "Display", tags: []string{"paragraph", "typography", "label"}, description: "A line or paragraph of plain text.",
		props:   `"text":{"type":"string"},"variant":{"type":"string","enum":["body1","body2","caption","overline","subtitle1","subtitle2"]},"color":{"type":"string"},` + sx,
		example: `{"type":"mui.text","key":"t","props":{"text":"Hello"}}`},
	{id: "mui.heading", title: "Heading", category: "Display", tags: []string{"title", "text", "typography"}, description: "A section heading, level 1 to 6.",
		props: `"text":{"type":"string"},"level":{"type":"integer","minimum":1,"maximum":6},` + sx, required: `"text"`,
		example: `{"type":"mui.heading","key":"h","props":{"text":"Hello","level":2}}`},
	{id: "mui.markdown", title: "Markdown", category: "Display", tags: []string{"text", "formatted", "rich text"}, description: "Read-only markdown, rendered as Foundation's Live Preview renders it.",
		props: `"text":{"type":"string"}`, example: `{"type":"mui.markdown","key":"m","props":{"text":"**Bold** text"}}`},
	{id: "mui.icon", title: "Icon", category: "Display", tags: []string{"symbol", "material icon"}, description: "A Material icon by name.",
		props: `"name":{"type":"string","description":"A Material Symbols name, e.g. waving_hand"},"color":{"type":"string"},"fontSize":{"type":"string","enum":["small","medium","large"]}`, required: `"name"`,
		example: `{"type":"mui.icon","key":"i","props":{"name":"star"}}`},
	{id: "mui.avatar", title: "Avatar", category: "Display", tags: []string{"user", "picture", "initials"}, description: "A round picture or initials for a person.",
		props: `"name":{"type":"string"},"src":{"type":"string"},"color":{"type":"string"}`, example: `{"type":"mui.avatar","key":"a","props":{"name":"Ada"}}`},
	{id: "mui.chip", title: "Chip", category: "Display", tags: []string{"tag", "label", "pill"}, description: "A compact label, optionally clickable or deletable.",
		props: label + `,"color":{"type":"string","enum":["default","primary","secondary","success","warning","error","info"]},"variant":{"type":"string","enum":["filled","outlined"]}`, required: `"label"`,
		events: ev("click", "The chip was clicked", "delete", "Its delete icon was clicked"), example: `{"type":"mui.chip","key":"c","props":{"label":"New"}}`},
	{id: "mui.badge", title: "Badge", category: "Display", tags: []string{"count", "indicator"}, description: "A small count or dot on its child.",
		props: `"content":{"description":"The count or text"},"color":{"type":"string"},"dot":{"type":"boolean"}`, children: true,
		example: `{"type":"mui.badge","key":"b","props":{"content":3},"children":[{"type":"mui.icon","key":"i","props":{"name":"mail"}}]}`},
	// Input
	{id: "mui.textField", title: "Text field", category: "Input", description: "A single- or multi-line text input.",
		props:   label + `,` + value + `,"placeholder":{"type":"string"},"multiline":{"type":"boolean"},"rows":{"type":"integer"},"helperText":{"type":"string"},"error":{"type":"boolean"},"debounce":{"type":"integer","description":"Milliseconds change waits for typing to pause before it is sent; 0 sends every change"},` + disabled,
		events:  ev("change", change, "submit", "Enter was pressed (Ctrl+Enter when multiline): payload {value}", "blur", "The field lost focus: payload {value}"),
		example: `{"type":"mui.textField","key":"name","props":{"label":"Name"},"bind":{"value":"local.name"}}`, tags: []string{"form"}},
	{id: "mui.number", title: "Number field", category: "Input", description: "A numeric input with optional bounds and step.",
		props:  label + `,` + value + `,"min":{"type":"number"},"max":{"type":"number"},"step":{"type":"number"},` + disabled,
		events: ev("change", change, "submit", "Enter was pressed: payload {value}"), example: `{"type":"mui.number","key":"n","props":{"label":"Amount"}}`, tags: []string{"form"}},
	{id: "mui.select", title: "Select", category: "Input", description: "Picks one of several options.",
		props:    label + `,` + value + `,"options":{"type":"array","items":{"type":"object","properties":{"value":{"type":"string"},"label":{"type":"string"}}}},` + disabled,
		required: `"options"`, events: ev("change", "An option was picked: payload {value}"),
		example: `{"type":"mui.select","key":"s","props":{"label":"Colour","options":[{"value":"r","label":"Red"}]}}`, tags: []string{"form"}},
	{id: "mui.checkbox", title: "Checkbox", category: "Input", description: "A checkbox with a label.",
		props: label + `,"checked":{"type":"boolean"},` + disabled, events: ev("change", "It was ticked or cleared: payload {value}"),
		example: `{"type":"mui.checkbox","key":"c","props":{"label":"Done"}}`, tags: []string{"form"}},
	{id: "mui.switch", title: "Switch", category: "Input", description: "An on/off switch with a label.",
		props: label + `,"checked":{"type":"boolean"},` + disabled, events: ev("change", "It was switched: payload {value}"),
		example: `{"type":"mui.switch","key":"s","props":{"label":"Enabled"}}`, tags: []string{"form"}},
	{id: "mui.date", title: "Date and time", category: "Input", description: "A date, time or date-and-time input; values are ISO 8601 strings.",
		props: label + `,` + value + `,"mode":{"type":"string","enum":["date","time","datetime"]},` + disabled, events: ev("change", "The value changed: payload {value}"),
		example: `{"type":"mui.date","key":"d","props":{"label":"Due","mode":"date"}}`, tags: []string{"form"}},
	{id: "mui.autocomplete", title: "Autocomplete", category: "Input", description: "A text input that suggests options as you type; one or several values.",
		props:    label + `,` + value + `,"options":{"type":"array","items":{"type":"object","properties":{"value":{"type":"string"},"label":{"type":"string"}}}},"multiple":{"type":"boolean"},"freeSolo":{"type":"boolean"},` + disabled,
		required: `"options"`, events: ev("change", "The chosen value(s) changed: payload {value}", "input", "The typed text changed: payload {value}, for loading options"),
		example: `{"type":"mui.autocomplete","key":"a","props":{"label":"Tags","multiple":true,"options":[]}}`, tags: []string{"form"}},
	// Actions
	{id: "mui.button", title: "Button", category: "Action", tags: []string{"click", "action", "submit"}, description: "A button that sends an event when clicked.",
		props: label + `,"variant":{"type":"string","enum":["text","outlined","contained"]},"color":{"type":"string"},"icon":{"type":"string"},` + disabled, required: `"label"`,
		events: ev("click", "It was clicked; the payload holds what the mapping names"), example: `{"type":"mui.button","key":"save","props":{"label":"Save","variant":"contained"},"on":{"click":{"handler":"h_save"}}}`},
	{id: "mui.iconButton", title: "Icon button", category: "Action", tags: []string{"button", "icon", "action"}, description: "A round button showing an icon, with its title as tooltip and accessible name.",
		props: `"icon":{"type":"string"},"title":{"type":"string"},"color":{"type":"string"},` + disabled, required: `"icon","title"`,
		events: ev("click", "It was clicked"), example: `{"type":"mui.iconButton","key":"del","props":{"icon":"delete","title":"Delete"}}`},
	{id: "mui.menu", title: "Menu", category: "Action", tags: []string{"dropdown", "actions", "options"}, description: "A button opening a menu of items.",
		props: label + `,"icon":{"type":"string"},"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"icon":{"type":"string"}}}}`, required: `"items"`,
		events: ev("select", "An item was chosen: payload {id}"), example: `{"type":"mui.menu","key":"m","props":{"icon":"more_vert","items":[{"id":"rename","label":"Rename"}]}}`},
	// Collections
	{id: "mui.list", title: "List", category: "Collection", tags: []string{"items", "rows"}, description: "A vertical list of items with a primary and secondary line, optionally selectable.",
		props:   `"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"primary":{"type":"string"},"secondary":{"type":"string"},"icon":{"type":"string"}}}},"selected":{"type":"string"},"dense":{"type":"boolean"}`,
		events:  ev("select", "An item was clicked: payload {id}", "open", "An item was double-clicked: payload {id}"),
		example: `{"type":"mui.list","key":"l","props":{"items":[{"id":"1","primary":"First"}]}}`},
	{id: "mui.table", title: "Table", category: "Collection", tags: []string{"grid", "rows", "columns", "data"}, description: "A virtualized table with sortable columns, row selection, paging and checkbox cells as events. A column's field may be a dotted path (_expanded.assignee.displayName); a bool column shows checkboxes, editable ones sending edit; an enum column shows its options' labels.",
		props:    `"columns":{"type":"array","items":{"type":"object","properties":{"field":{"type":"string","description":"The row's value, by a dotted path"},"title":{"type":"string"},"width":{"type":"number"},"sortable":{"type":"boolean"},"sortField":{"type":"string","description":"The field the sort event names; field when left out"},"type":{"type":"string","enum":["text","number","bool","date","dateTime","enum","actions"],"description":"actions: the column's actions as buttons, each sending action"},"recordField":{"type":"object","description":"A field of an entity type (entity.describe's fields): the cell shows the value by the field's type, as record.fieldValue does"},"actions":{"type":"array","description":"An actions column's buttons","items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"icon":{"type":"string"}}}},"editable":{"type":"boolean","description":"A bool cell's checkbox sends edit"},"options":{"type":"array","items":{"type":"object","properties":{"value":{"type":"string"},"label":{"type":"string"}}}}}}},"rows":{"type":"array","items":{"type":"object"}},"rowKey":{"type":"string"},"sort":{"type":"object","properties":{"field":{"type":"string"},"direction":{"type":"string","enum":["asc","desc"]}}},"selected":{"type":"array"},"selection":{"type":"string","enum":["none","single","multiple"]},"total":{"type":"integer"},"height":{"type":"number"}`,
		required: `"columns","rows"`, events: ev("sort", "A column header was clicked: payload {field, direction}", "select", "The selection changed: payload {selected}", "open", "A row was double-clicked: payload {key}", "page", "More rows are wanted: payload {offset}", "edit", "An editable cell was changed: payload {key, field, value}", "action", "An actions column's button was clicked: payload {key, action}"),
		example: `{"type":"mui.table","key":"t","props":{"columns":[{"field":"name","title":"Name"}],"rows":[{"id":"1","name":"Ada"}]}}`},
	// Containers
	{id: "mui.tabs", title: "Tabs", category: "Container", tags: []string{"navigation", "sections"}, description: "Plain tabs: one child per tab, keyed by the tab's value; only the selected one is shown.",
		props: `"tabs":{"type":"array","items":{"type":"object","properties":{"value":{"type":"string"},"label":{"type":"string"}}}},"selected":{"type":"string"}`, required: `"tabs"`,
		children: true, events: ev("change", "A tab was chosen: payload {value}"),
		example: `{"type":"mui.tabs","key":"t","props":{"tabs":[{"value":"a","label":"A"}],"selected":"a"},"children":[{"type":"mui.text","key":"a","props":{"text":"A"}}]}`},
	{id: "mui.dialog", title: "Dialog", category: "Container", tags: []string{"modal", "popup", "overlay"}, description: "A modal dialog shown while open is true; its actions slot holds the buttons.",
		props: `"open":{"type":"boolean"},"title":{"type":"string"},"maxWidth":{"type":"string","enum":["xs","sm","md","lg","xl"]}`, children: true, slots: []string{"actions"},
		events: ev("close", "The user dismissed it"), example: `{"type":"mui.dialog","key":"d","props":{"open":false,"title":"Rename"}}`},
	{id: "mui.drawer", title: "Drawer", category: "Container", tags: []string{"side panel", "overlay"}, description: "A panel sliding in from an edge while open is true.",
		props: `"open":{"type":"boolean"},"anchor":{"type":"string","enum":["left","right","top","bottom"]},"width":{"type":"number"}`, children: true,
		events: ev("close", "The user dismissed it"), example: `{"type":"mui.drawer","key":"d","props":{"open":false,"anchor":"right"}}`},
	// Feedback
	{id: "mui.progress", title: "Progress", category: "Feedback", tags: []string{"loading", "spinner", "bar"}, description: "A linear or circular progress indicator, indeterminate without a value.",
		props: `"variant":{"type":"string","enum":["linear","circular"]},"value":{"type":"number","minimum":0,"maximum":100}`, example: `{"type":"mui.progress","key":"p"}`},
	{id: "mui.alert", title: "Alert", category: "Feedback", tags: []string{"message", "notice", "banner"}, description: "A message with a severity.",
		props: `"severity":{"type":"string","enum":["success","info","warning","error"]},"title":{"type":"string"},"text":{"type":"string"}`, required: `"text"`,
		events: ev("close", "Its close button was clicked"), example: `{"type":"mui.alert","key":"a","props":{"severity":"info","text":"Saved"}}`},
	{id: "mui.tooltip", title: "Tooltip", category: "Feedback", tags: []string{"hint", "hover"}, description: "Shows a hint over its one child on hover.",
		props: `"title":{"type":"string"}`, required: `"title"`, children: true,
		example: `{"type":"mui.tooltip","key":"t","props":{"title":"Hint"},"children":[{"type":"mui.icon","key":"i","props":{"name":"help"}}]}`},
	{id: "mui.empty", title: "Empty state", category: "Feedback", tags: []string{"placeholder", "no data"}, description: "What an empty list or page shows: an icon, a title, a line of text and an optional action.",
		props: `"icon":{"type":"string"},"title":{"type":"string"},"text":{"type":"string"},"action":{"type":"string","description":"The action button's label"}`, required: `"title"`,
		events: ev("action", "The action button was clicked"), example: `{"type":"mui.empty","key":"e","props":{"title":"No items yet"}}`},
	{id: "presence.avatars", title: "Avatars of who is here", category: "Feedback", tags: []string{"presence", "collaboration"},
		description: "The avatars of the users in an object, each in their colour with their name on hover. Bind users to presence:<object>; join makes this window present in an object while the node is shown, so others see it.",
		props:       `"users":{"type":"array","description":"Presence entries, as a presence: binding gives them: {connection, userId, name, color}","items":{"type":"object"}},"join":{"type":"string","description":"An object (\"<type>.<id>\") this window is in while it is shown; empty joins none"},"max":{"type":"integer","description":"The most avatars shown before +n"},"self":{"type":"boolean","description":"Show this window's own connection too"}`,
		example:     `{"type":"presence.avatars","key":"here","props":{"join":"record_task.123"},"bind":{"users":"presence:props.object"}}`},
	// Navigation
	{id: "mui.breadcrumbs", title: "Breadcrumbs", category: "Navigation", tags: []string{"navigation", "path", "trail"}, description: "The path to the current page, each item but the last a link.",
		props: `"items":{"type":"array","items":{"type":"object","properties":{"label":{"type":"string"},"id":{"type":"string","description":"What navigate names; the label when left out"}},"required":["label"]}}`, required: `"items"`,
		events:  ev("navigate", "An item was clicked: payload {id, index}"),
		example: `{"type":"mui.breadcrumbs","key":"crumbs","props":{"items":[{"label":"Contacts","id":"contacts"},{"label":"Ada Lovelace"}]}}`},
	// Records
	{id: "record.fieldEditor", title: "Field editor", category: "Records", tags: []string{"form", "record"},
		description: "The input for one field of an entity type, chosen by the field's type: text, long text, rich text, number, decimal, money, bool, date, date and time, duration, enum, multi-enum, tags, relation, user, file, JSON, list and object; computed fields show their value read-only. Bind field to one of entity.describe's fields and value to the record's value.",
		props:       `"field":{"type":"object","description":"The field, as entity.describe gives it: {id, name, type, options, relation, scale, currency, items, fields, …}"},"value":{"description":"The value, as the record holds it"},"problem":{"type":"string","description":"Why the value was refused, shown under the input"},"label":{"type":"string","description":"The field's name when left out"},"entity":{"type":"string","description":"The entity type, for uploading a file field's file"},` + disabled,
		required:    `"field"`, events: ev("change", "A new value was entered: payload {value}, in the record's format"),
		example: `{"type":"record.fieldEditor","key":"title","props":{"field":{"id":"title","name":"Title","type":"text"}},"bind":{"value":"props.task.title"}}`},
	{id: "record.fieldValue", title: "Field value", category: "Records", tags: []string{"record"},
		description: "A field's value shown by its type: a date as a date, an enum as its option's chip, money with its currency, a relation as the related records' titles (from a query's expand), a file as a link.",
		props:       `"field":{"type":"object","description":"The field, as entity.describe gives it"},"value":{"description":"The value, as the record holds it"},"expanded":{"description":"For a relation, the related record or records (the record's _expanded.<field>)"},"entity":{"type":"string","description":"The entity type, for a file field's link"},"record":{"type":"string","description":"The record's id, for a file field's link"}`,
		required:    `"field"`, example: `{"type":"record.fieldValue","key":"due","props":{"field":{"id":"due","name":"Due","type":"date"},"value":"2026-11-15"}}`},
	{id: "record.form", title: "Record form", category: "Records", tags: []string{"form", "record", "collaboration"},
		description: "Edits one record: a field editor per field, with Save and Cancel. It keeps what this user typed until saved, so the record's other fields update in place as other people change them; a field both changed shows the other's value and name, to keep or take. save sends only the fields this user changed.",
		props:       `"fields":{"type":"array","items":{"type":"object"},"description":"The fields to edit, as entity.describe gives them; hidden ones are skipped and computed ones shown read-only"},"record":{"type":"object","description":"The record as a records.get query keeps it, so it follows other people's changes"},"entity":{"type":"string","description":"The entity type, for file uploads"},"problems":{"type":"object","description":"Field id → why its value was refused"},"users":{"type":"array","items":{"type":"object"},"description":"Presence entries (presence:<object>), to name the user whose change a field conflicts with"},"saveLabel":{"type":"string"},"cancelLabel":{"type":"string","description":"No Cancel button when empty"},` + disabled,
		required:    `"fields"`, events: ev("save", "Save was clicked with changes: payload {set: field id → value, only the fields this user changed, seq: the record's _seq it was based on}", "cancel", "Cancel was clicked; what was typed is dropped"),
		example: `{"type":"record.form","key":"form","props":{"fields":[{"id":"title","name":"Title","type":"text"}]},"bind":{"record":"query:record"}}`},
	// Connectors (forge 07c)
	{id: "connection.form", title: "Connection form", category: "Connectors", tags: []string{"form", "connector", "connection", "settings"},
		description: "Makes or edits a connection: picks a connector type implementing contract (an offline one shown as such), draws its settings from the type's settings schema, shows its secrets as write-only fields (a stored one only as set; empty keeps it), and has Test (connection.test) and Save (connection.create or connection.update). Generic over every connector type.",
		props:       `"connection":{"type":"string","description":"The connection to edit; a new one when empty"},"contract":{"type":"string","description":"Only connector types implementing this contract, id@version"},"owner":{"type":"string","enum":["me","tenant"],"description":"Who owns a new connection: me (the default) or the tenant"},"saveLabel":{"type":"string"},` + disabled,
		events:      ev("saved", "The connection was saved: payload {id}", "tested", "Test finished: payload {ok, error {code, message}}"),
		example:     `{"type":"connection.form","key":"inbound","props":{"contract":"mail.mailbox@1"}}`},
	// flexlayout
	{id: "layout.flex", title: "Docked sub-tabs", category: "Layout", tags: []string{"flexlayout", "tabs"},
		description: "An application's own nested, dockable sub-tabs (flexlayout). props.layout is the model: rows of tab sets of tabs; each tab's content is the child whose key is the tab's id, rendered when the tab is shown. The user may rearrange the tabs; that arrangement is kept per user and page.",
		props:       `"layout":{"type":"object","description":"{row: {type: 'row', children: [{type: 'tabset', weight?, selected?, children: [{type: 'tab', id, name, closable?}]}, …]}}"}`, required: `"layout"`,
		children: true, events: ev("select", "A tab was selected: payload {id}", "close", "A tab was closed: payload {id}", "move", "A tab was moved: payload {id, to}"),
		example: `{"type":"layout.flex","key":"tabs","props":{"layout":{"row":{"type":"row","children":[{"type":"tabset","children":[{"type":"tab","id":"a","name":"A"}]}]}}},"children":[{"type":"mui.text","key":"a","props":{"text":"A"}}]}`},
	// React Flow
	{id: "graph.canvas", title: "Graph canvas", category: "Graph", tags: []string{"react flow", "workflow", "nodes"},
		description: "A graph editor (React Flow). Nodes have an id, a position, a label and ports; a child keyed by a node's id is drawn as its body, so a node can show a form. Positions live in the browser while dragging and are sent on drag end. Other users' selections are outlined in their colour.",
		props:       `"nodes":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"position":{"type":"object"},"label":{"type":"string"},"type":{"type":"string"},"ports":{"type":"object","description":"{in: [names], out: [names]}"}}}},"edges":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"source":{"type":"string"},"target":{"type":"string"},"sourceHandle":{"type":"string"},"targetHandle":{"type":"string"}}}},"mode":{"type":"string","enum":["free","chain"]},"palette":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"}}}},"selected":{"type":"array"},"height":{"type":"number"}`,
		required:    `"nodes","edges"`, children: true,
		events: ev("connect", "Two ports were joined: payload {source, sourceHandle, target, targetHandle}", "disconnect", "An edge was removed: payload {id}",
			"move", "A node was dropped after a drag: payload {id, position}", "select", "The selection changed: payload {nodes, edges}",
			"delete", "Nodes or edges were deleted: payload {nodes, edges}", "drop", "A palette item was dropped: payload {item, position}", "open", "A node was double-clicked: payload {id}"),
		example: `{"type":"graph.canvas","key":"g","props":{"nodes":[{"id":"a","position":{"x":0,"y":0},"label":"A","ports":{"out":["out"]}}],"edges":[]}}`},
	// The stand-in for a node that failed validation
	{id: "ui.error", title: "Error box", category: "Feedback", tags: []string{"error", "validation"}, description: "Shown in place of a node that failed validation (an unknown component or invalid props), with what was wrong.",
		props: `"messages":{"type":"array","items":{"type":"string"}}`, example: `{"type":"ui.error","key":"x","props":{"messages":["there is no component \"mui.nope\""]}}`},
}

// Contribute registers every primitive; it fails on a component missing
// its metadata.
func Contribute(reg *extensions.Registry) error {
	for _, s := range specs {
		required := ""
		if s.required != "" {
			required = `,"required":[` + s.required + `]`
		}
		props := json.RawMessage(`{"type":"object","properties":{` + s.props + `}` + required + `}`)
		if err := ui.RegisterComponent(reg, ui.Component{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "ui"}, Title: s.title, Description: s.description, Category: s.category, Tags: s.tags,
			Props: props, Events: s.events, Children: s.children, Slots: s.slots, Examples: []json.RawMessage{json.RawMessage(s.example)},
		}); err != nil {
			return err
		}
	}
	return nil
}
