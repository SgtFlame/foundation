package uicomponents

// mui.kanban (forge 30b) is kept in its own file, appended to specs, so
// parallel work on other primitives does not touch the same lines.
func init() {
	specs = append(specs, spec{id: "mui.kanban", title: "Kanban board", category: "Collection",
		tags: []string{"kanban", "board", "columns", "cards", "drag", "drop", "tasks", "status", "pipeline"},
		description: "Columns of cards dragged between and within columns (forge 30b). Its one child is the card template, evaluated per card under as (a repeat's row), so a card is a prefab people can edit. " +
			"cards are records (bind a records.find page sorted by the rank field); columnField names the field a card's column is read from. Columns are {id, title, color, count, limit}. " +
			"A drop sends one move {id, from, to, index, after, before}, after and before being the cards it was dropped between (for record.move): the blueprint decides what a drop means. " +
			"The dropped card stays where it was dropped until its record changes (or 5 s pass, then it goes back), and a change by someone else to a held card wins. Others' moves slide into place. " +
			"Keyboard: Space lifts a card, arrows move it, Space drops it and Escape puts it back. Columns are windowed, so thousands of cards scroll smoothly. Inside a prefab the server sends the cards as placed {id, column, version, key}, key naming the card's rendered child.",
		props: `"cards":{"type":"array","description":"The cards: records (or {_id, …}) in their order within a column; a records.find page is taken as its records","items":{"type":"object"}},` +
			`"columns":{"type":"array","description":"The columns in order: {id, title, color, count, limit}","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"color":{"type":"string","description":"A theme palette name or a CSS colour"},"count":{"type":"integer","description":"How many cards it holds, when more than it shows"},"limit":{"type":"integer","description":"A work-in-progress limit, shown when passed"}}}},` +
			`"columnField":{"type":"string","description":"The card field naming its column, status when left out"},` +
			`"titleField":{"type":"string","description":"The field a card without a template shows, title (else _title) when left out"},` +
			`"as":{"type":"string","description":"The card's name in the template; card when left out"},` +
			`"cardHeight":{"type":"number","description":"Pixels a card takes before it is measured, 72 when left out"},` +
			`"columnWidth":{"type":"number","description":"Pixels, 280 when left out"},` +
			`"height":{"type":"number","description":"Pixels; fills its box when left out"},` +
			`"readOnly":{"type":"boolean","description":"Cards cannot be dragged"},` +
			`"collapsed":{"type":"array","items":{"type":"string"},"description":"Ids of columns shown narrow"},` +
			`"addable":{"type":"boolean","description":"Each column has an add button sending add"},` +
			`"reorderColumns":{"type":"boolean","description":"Columns can be dragged by their headers, sending columnMove"},` +
			`"busy":{"type":"object","description":"cardId → [{name}] of others on that card (presence), shown as a ring"},` +
			`"placed":{"type":"array","description":"Set by the server inside a prefab: {id, column, version, key} per card, key naming its rendered child","items":{"type":"object"}},` + sx,
		children: true,
		events: ev("move", "A card was dropped: payload {id, from, to, index, after, before} (after and before the ids of the cards above and below it in to, empty at an end)",
			"open", "A card was clicked or Enter pressed on it: payload {id}",
			"add", "A column's add button was pressed: payload {column}",
			"columnMove", "A column was dragged by its header: payload {id, index}",
			"lift", "A card was lifted or put down: payload {id} (empty when put down), for presence"),
		example: `{"type":"mui.kanban","key":"board","props":{"columnField":"status","as":"card","columns":[{"id":"todo","title":"To do"},{"id":"done","title":"Done"}]},"bind":{"cards":"query:tasks.records"},"on":{"move":{"handler":"onMove","payload":{"id":"event.id","to":"event.to","after":"event.after","before":"event.before"}}},"children":[{"type":"mui.text","key":"title","bind":{"text":"card.title"}}]}`,
	})
}
