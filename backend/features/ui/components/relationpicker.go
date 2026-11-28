package uicomponents

// mui.relationPicker (forge 40b) is kept in its own file, appended to specs,
// so parallel work on other primitives does not touch the same lines.
func init() {
	specs = append(specs, spec{id: "mui.relationPicker", title: "Relation picker", category: "Input",
		tags: []string{"relation", "picker", "lookup", "search", "autocomplete", "record", "select", "form"},
		description: "Picks one or several records of an entity type by searching as you type over its title (forge 40b): records.search when the type has searchable fields (and searchable is not false), else a prefix scan of the title field through records.find, debounced, the first records by title before anything is typed. " +
			"value is a record id, or a list of ids when multiple; the chosen records are shown by title and kept live. where narrows the choices (contacts of one company). With create, a Create \"…\" row sends create {text} for text no shown record is titled, for the blueprint to make the record and set it. " +
			"record.fieldEditor and record.form use it for relation fields.",
		props: `"entity":{"type":"string","description":"The entity type whose records are picked"},` +
			label + `,` + value + `,` +
			`"multiple":{"type":"boolean","description":"Several records; value is then a list of ids"},` +
			`"where":{"type":"object","description":"Field id → value the choices must have, such as {\"company\": \"<id>\"}"},` +
			`"searchable":{"type":"boolean","description":"false scans the title prefix even when the type has searchable fields"},` +
			`"create":{"type":"boolean","description":"Offer Create \"…\" for typed text, sending create"},` +
			`"display":{"type":"array","items":{"type":"string"},"description":"Field ids shown under each choice's title, such as [\"email\"]"},` +
			`"placeholder":{"type":"string"},"helperText":{"type":"string","description":"A line under it, shown as an error"},` + disabled + `,` + sx,
		required: `"entity"`,
		events: ev("change", "The chosen records changed: payload {value}, an id (or null) or a list of ids when multiple",
			"create", "Create \"…\" was chosen: payload {text}"),
		example: `{"type":"mui.relationPicker","key":"company","props":{"entity":"company","label":"Company","create":true,"display":["domain"]},"bind":{"value":"local.company"},"on":{"change":{"handler":"onCompany","payload":{"company":"event.value"}},"create":{"handler":"onNewCompany","payload":{"name":"event.text"}}}}`,
	})
}
