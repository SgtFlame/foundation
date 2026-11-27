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
		props: grow + `,"fill":{"type":"boolean","description":"Fill the height it is given, as a frame's page does (a slot's props style the box its content is put in)"},"size":{"type":"integer","minimum":1,"maximum":12,"description":"Its width in a mui.grid's 12 columns (forge 18c); 12 when left out"},` + sx, children: true, example: `{"type":"mui.box","key":"b","props":{"sx":{"p":2}}}`},
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
		props:   label + `,` + value + `,"placeholder":{"type":"string"},"multiline":{"type":"boolean"},"rows":{"type":"integer"},"helperText":{"type":"string"},"error":{"type":"boolean"},"debounce":{"type":"integer","description":"Milliseconds change waits for typing to pause before it is sent; 0 sends every change"},"clearOnSubmit":{"type":"boolean","description":"Empty the field once submit is sent, as a message box does (forge 18c)"},` + disabled,
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
	{id: "mui.list", title: "List", category: "Collection", tags: []string{"items", "rows"}, description: "A vertical list of items with a primary and secondary line, optionally selectable; items may carry a trailing count and an indent under the item above, and header items title the sections.",
		props:   `"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"primary":{"type":"string"},"secondary":{"type":"string"},"icon":{"type":"string"},"count":{"type":"number","description":"A trailing count, such as unread messages; the item is bold while it is above 0"},"header":{"type":"boolean","description":"A section title over the items after it, not selectable"},"indent":{"type":"integer","minimum":0,"maximum":3,"description":"Nesting under the item above (0-3), such as each agent's queue under Needs review"}}}},"selected":{"type":"string"},"dense":{"type":"boolean"}`,
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
		props:       `"users":{"type":"array","description":"Presence entries, as a presence: binding gives them: {connection, userId, name, color}","items":{"type":"object"}},"join":{"type":"string","description":"An object (\"<type>.<id>\") this window is in while it is shown; empty joins none"},"max":{"type":"integer","description":"The most avatars shown before +n"},"self":{"type":"boolean","description":"Show this window's own connection too"},"state":{"type":"object","description":"With join: what this window is doing there, such as {tab: \"draft\"}, read by others in each presence entry's state; merged with what other parts of the page put there"}`,
		example:     `{"type":"presence.avatars","key":"here","props":{"join":"record_task.123"},"bind":{"users":"presence:props.object"}}`},
	// Navigation
	{id: "mui.breadcrumbs", title: "Breadcrumbs", category: "Navigation", tags: []string{"navigation", "path", "trail"}, description: "The path to the current page, each item but the last a link.",
		props: `"items":{"type":"array","items":{"type":"object","properties":{"label":{"type":"string"},"id":{"type":"string","description":"What navigate names; the label when left out"}},"required":["label"]}}`, required: `"items"`,
		events:  ev("navigate", "An item was clicked: payload {id, index}"),
		example: `{"type":"mui.breadcrumbs","key":"crumbs","props":{"items":[{"label":"Contacts","id":"contacts"},{"label":"Ada Lovelace"}]}}`},
	// Records
	{id: "record.fieldEditor", title: "Field editor", category: "Records", tags: []string{"form", "record"},
		description: "The input for one field of an entity type, chosen by the field's type: text, long text, markdown (doc.markdown, collaborative, given entity and record), rich text, number, decimal, money, bool, date, date and time, duration, enum, multi-enum, tags, relation, user, file, JSON, list and object; computed fields show their value read-only. Bind field to one of entity.describe's fields and value to the record's value.",
		props:       `"field":{"type":"object","description":"The field, as entity.describe gives it: {id, name, type, options, relation, scale, currency, items, fields, …}"},"value":{"description":"The value, as the record holds it"},"problem":{"type":"string","description":"Why the value was refused, shown under the input"},"label":{"type":"string","description":"The field's name when left out"},"entity":{"type":"string","description":"The entity type, for uploading a file field's file and a markdown field's room"},"record":{"type":"string","description":"The record's id: a markdown field is edited together (doc.markdown) when it is set"},` + disabled,
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
	// Content components (forge 07l)
	{id: "mui.htmlView", title: "HTML view", category: "Display", tags: []string{"html", "email", "untrusted", "sandbox", "newsletter"},
		description: "Shows HTML that is not trusted (a mail message, a newsletter) in a sandboxed frame that runs no script, submits no form and loads nothing from the network: images and backgrounds from data: URLs and the tenant's record files only, until allowRemote. A Show images bar raises allowRemote when the HTML has remote images (data-src, as the MIME codec leaves them, or src). Quoted text (blockquote, Gmail and Outlook quote markers) is folded behind a … button when collapseQuotes. Links open in a new window, through a warning when a link's text names another site than its target. The frame grows to its content up to maxHeight, then scrolls.",
		props:       `"html":{"type":"string","description":"The HTML; it is sanitized again in the browser whatever its source"},"allowRemote":{"type":"boolean","description":"Load remote images; bind it to state the allowRemote event sets"},"collapseQuotes":{"type":"boolean","description":"Fold quoted text behind …; true when left out"},"maxHeight":{"type":"number","description":"Pixels the frame grows to before it scrolls; none when left out"}`,
		events:      ev("allowRemote", "Show images was clicked: payload {}. Set allowRemote, or remember the sender", "link", "A link was opened: payload {href, text, mismatched}"),
		example:     `{"type":"mui.htmlView","key":"body","props":{"html":"<p>Hello <b>there</b></p>"},"bind":{"allowRemote":"state.showImages"}}`},
	{id: "doc.markdown", title: "Markdown editor", category: "Input", tags: []string{"markdown", "editor", "collaboration", "document", "rich text"},
		description: "Edits a record's markdown field together with everyone who has it open: typing is shared as it happens through a Yjs room for the record and field, with each author's cursor in their colour, and the room's writer saves the field with record.update a second after typing pauses. Without record.update on the record it opens read-only. The toolbar has bold, italic, heading, list, quote, code and link. Given value and no record, it shows that text read-only (a version). Highlights (forge 18c) mark places in the text, each placed by its Yjs relative positions while the room is open, else its range while that still holds its quote, else its quote found again under its headings; they move as anyone types, and clicking one raises highlightClicked. Selecting text raises selected with the quote, its heading path and the words around it, which a page uses to start a thread or a suggestion there.",
		props:       `"entity":{"type":"string","description":"The entity type"},"id":{"type":"string","description":"The record's id"},"field":{"type":"string","description":"The markdown field's id"},"placeholder":{"type":"string"},"toolbar":{"type":"boolean","description":"Show the formatting toolbar; true when left out"},"minHeight":{"type":"number","description":"Pixels, 120 when left out"},"maxHeight":{"type":"number","description":"Pixels it grows to before it scrolls"},"typingIn":{"type":"string","description":"An object (\"<type>.<id>\") this window is present in while the editor is shown, with typingState while it typed in the last few seconds: others read it from presence: to show who is replying"},"typingState":{"type":"object","description":"The presence state while typing; {typing: true} when left out"},"value":{"type":"string","description":"Text to show read-only when no record is given, such as a committed version"},"highlights":{"type":"array","description":"Marks over the text: [{id, range: {from, to}, anchor: {quote, headingPath, context, ypos: {from, to}}, kind (anchored, suggestion, conflict), colour}]; ypos is what selected gave","items":{"type":"object"}},"focus":{"type":"object","description":"{id, n}: scrolls to the highlight id each time it changes"},` + disabled,
		required:    ``, events: ev("change", "The text changed, debounced to a second after typing pauses: payload {value}. The field is saved by the room; this is for previews and counts", "selected", "The selection settled: payload {quote, headingPath, context, range: {from, to}, ypos: {from, to} (Yjs relative positions, with a room)}; quote is empty when the selection was cleared", "highlightClicked", "A highlight was clicked: payload {id}"),
		example: `{"type":"doc.markdown","key":"body","props":{"entity":"note","field":"body"},"bind":{"id":"props.note._id"}}`},
	{id: "mui.virtualList", title: "Virtual list", category: "Collection", tags: []string{"list", "virtualized", "infinite scroll", "records", "inbox"},
		description: "A scrolling list over an entity type's records, as many as there are (records.find with where, filter and sort): the server renders only a window of about three screens, its one child being the row template evaluated per record under as (a repeat's row), and moves the window as the user scrolls. Rows stay current as others change them. Selection (one, or many with Ctrl/Shift for a range), arrow keys, Home/End, Enter to open. Inside a prefab only.",
		props:       `"entity":{"type":"string","description":"The entity type"},"where":{"type":"object","description":"Field id → value"},"filter":{"type":"object","description":"{field, op, value}, or {and|or: [filters]}"},"sort":{"type":"array","items":{"type":"object","properties":{"field":{"type":"string"},"desc":{"type":"boolean"}}}},"fields":{"type":"array","items":{"type":"string"}},"expand":{"type":"array","items":{"type":"string"}},"as":{"type":"string","description":"The row's name in the template; row when left out"},"rowHeight":{"type":"number","description":"Pixels per row, 48 when left out"},"height":{"type":"number","description":"Pixels; fills its container when left out"},"width":{"type":"number","description":"Pixels; fills its container when left out"},"pageSize":{"type":"integer","description":"Records per page the window is held in, 50 when left out"},"selection":{"type":"string","enum":["none","single","multiple"]},"selected":{"type":"array","description":"Selected record ids"},"count":{"type":"integer","description":"Set by the server: the records in all"},"offset":{"type":"integer","description":"Set by the server: the index of the window's first row"},"ids":{"type":"array","description":"Set by the server: the window's record ids, a child per id"},"loading":{"type":"boolean","description":"Set by the server"},"reverse":{"type":"boolean","description":"A chat's message list (forge 26d-2): the first record (the newest, with a descending sort) at the bottom, rows as tall as their content (rowHeight their least), opening at the bottom and following new records while scrolled there (else a New messages button), endReached when the oldest is in view at the top"}`,
		required:    `"entity"`, children: true,
		events:  ev("select", "The selection changed: payload {selected, id}", "open", "A row was double-clicked or Enter pressed: payload {id}", "endReached", "The last record was scrolled into view: payload {count}", "window", "Sent by the list itself: the rows it shows, {first, last}, so the server moves its window"),
		example: `{"type":"mui.virtualList","key":"threads","props":{"entity":"thread","sort":[{"field":"lastAt","desc":true}],"as":"thread","selection":"multiple"},"children":[{"type":"mui.text","key":"subject","bind":{"text":"thread.subject"}}]}`},
	{id: "mui.virtualGrid", title: "Virtual grid", category: "Collection", tags: []string{"table", "grid", "datagrid", "virtualized", "records", "columns"},
		description: "A table over an entity type's records, as many as there are: the server holds a window of about three screens of records.find pages and sends their records as rows, and the browser draws the columns' cells (as mui.table's columns). Rows stay current as others change them. Sortable headers send sort; selection, arrow keys and Enter as mui.virtualList. Inside a prefab only.",
		props:       `"entity":{"type":"string","description":"The entity type"},"where":{"type":"object"},"filter":{"type":"object"},"sort":{"type":"array","items":{"type":"object","properties":{"field":{"type":"string"},"desc":{"type":"boolean"}}}},"fields":{"type":"array","items":{"type":"string"}},"expand":{"type":"array","items":{"type":"string"}},"columns":{"type":"array","description":"As mui.table's: {field, title, width, sortable, type, recordField, options}","items":{"type":"object"}},"rowHeight":{"type":"number","description":"Pixels per row, 36 when left out"},"height":{"type":"number"},"width":{"type":"number"},"pageSize":{"type":"integer"},"selection":{"type":"string","enum":["none","single","multiple"]},"selected":{"type":"array"},"count":{"type":"integer","description":"Set by the server"},"offset":{"type":"integer","description":"Set by the server"},"ids":{"type":"array","description":"Set by the server"},"rows":{"type":"array","items":{"type":"object"},"description":"Set by the server: the window's records"},"loading":{"type":"boolean","description":"Set by the server"}`,
		required:    `"entity","columns"`,
		events:      ev("sort", "A sortable header was clicked: payload {field, desc}", "select", "The selection changed: payload {selected, id}", "open", "A row was double-clicked or Enter pressed: payload {id}", "endReached", "The last record was scrolled into view: payload {count}", "window", "Sent by the grid itself: the rows it shows, {first, last}"),
		example:     `{"type":"mui.virtualGrid","key":"grid","props":{"entity":"contact","columns":[{"field":"name","title":"Name"}]}}`},
	{id: "mui.chipsInput", title: "Chips input", category: "Input", tags: []string{"chips", "tags", "addresses", "recipients", "people", "autocomplete"},
		description: "A list of values typed as chips: addresses, tags, people. Suggestions come from a query bound to suggestions, its text kept by the text event in state the query binds; free values are accepted when they match pattern. Enter, Tab, comma or semicolon make a chip; pasting a list splits it; Backspace on an empty input removes the last chip. A suggestion is {value, label, secondary}; a chip shows its label when one is known.",
		props:       label + `,"value":{"type":"array","items":{"type":"string"},"description":"The values"},"suggestions":{"type":"array","description":"Strings, or {value, label, secondary}","items":{}},"pattern":{"type":"string","description":"A regular expression a free value must match; with none, any text"},"free":{"type":"boolean","description":"Accept values that are not suggestions; true when left out"},"placeholder":{"type":"string"},"labels":{"type":"object","description":"value → label, for chips whose suggestion has gone"},` + disabled,
		events:      ev("change", "The values changed: payload {value}", "text", "The typed text changed, debounced: payload {text}, for a suggestions query", "invalid", "A typed value did not match pattern: payload {text}"),
		example:     `{"type":"mui.chipsInput","key":"to","props":{"label":"To","pattern":"^[^@\\s]+@[^@\\s]+$"},"bind":{"value":"state.to","suggestions":"query:people.records"}}`},
	{id: "chat.sources", title: "Sources", category: "Display", tags: []string{"citations", "sources", "chat", "agents", "retrieval", "passages"},
		description: "The passages a reply cites, drawn as a chat round's Sources row (forge 15b): each [n] with its title, lines and snippet. A click opens the Volume file at its lines, or the Chronicle entry, as a chat citation does; a memory entry opens only for its owner (location.owner), and anyone else sees its title without its text. For an agent run's segments or a draft's citations.",
		props:       `"segments":{"type":"array","description":"The segments: {number, sourceId, key, title, snippet, location: {volumeId, path, startLine, endLine, entryId, owner}}","items":{"type":"object"}},"title":{"type":"string","description":"Its heading, Sources when left out; empty shows none"},"collapsible":{"type":"boolean","description":"Shown as a Sources (n) button that opens the list, as in chat"},"empty":{"type":"string","description":"What it says when there are none; nothing when left out"}`,
		events:      ev("open", "A source was opened: payload {number, sourceId, key}"),
		example:     `{"type":"chat.sources","key":"sources","bind":{"segments":"state.run.segments"}}`},
	{id: "chat.composer", title: "Message composer", category: "Input", tags: []string{"chat", "message", "composer", "mentions", "emoji", "attachments", "channel"},
		description: "A Markdown message box for a channel or thread (forge 26d-2): @ suggests people and agents from mentions (bind it to a query), : suggests emoji, files are attached by the button, by paste or both and uploaded as record files of entity, Enter sends and Shift+Enter adds a line. send carries the text, the ids of the people it mentions (an @name nobody has stays plain text) and the uploaded files; typing is raised at most every three seconds while the text changes, for presence.",
		props:       label + `,"mentions":{"type":"array","description":"Who may be mentioned: {id, name (what follows @, spaces dropped), label, kind (user, agent)}","items":{"type":"object"}},"entity":{"type":"string","description":"The entity type whose file field the attachments go to; no attach button when left out"},"placeholder":{"type":"string"},` + disabled,
		events:      ev("send", "Enter or the send button: payload {text, mentions (ids, once each, in order), files ({id, name, size, contentType})}", "typing", "The text changed, at most every three seconds: payload {}"),
		example:     `{"type":"chat.composer","key":"composer","props":{"entity":"message","placeholder":"Message #design"},"bind":{"mentions":"query:members.records"},"on":{"send":{"handler":"sendMessage","payload":{"text":"event.text","mentions":"event.mentions","files":"event.files"}}}}`},
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
