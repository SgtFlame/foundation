package uicomponents

// mui.chart and mui.metric (forge 40a) are kept in their own file, appended
// to specs, so parallel work on other primitives does not touch the same lines.
func init() {
	specs = append(specs, spec{id: "mui.chart", title: "Chart", category: "Display",
		tags: []string{"chart", "graph", "plot", "bar", "line", "area", "pie", "funnel", "dashboard", "aggregate", "series"},
		description: "A bar, stacked bar, line, area, pie or funnel chart in the dashboard palette (forge 40a), drawn as SVG and animated when its data changes, so a live aggregate's update reads as movement. " +
			"Give it series [{key, label, points: [{x, y}]}], or rows with xField, yField and seriesField naming their fields, so records.aggregate's groups bind directly (rows {key: {stage: …}, values: {amount: …}} are read by dotted paths such as key.stage and values.amount). " +
			"At most seven series are drawn, the rest folded into a grey Other. format shows values as numbers, money (with currency), percents or durations (seconds). A click on a bar, point or slice sends select {series, x} for drill-down.",
		props: `"kind":{"type":"string","enum":["bar","stackedBar","line","area","pie","funnel"],"description":"bar when left out"},` +
			`"series":{"type":"array","description":"The series: {key, label, points: [{x, y}]}","items":{"type":"object","properties":{"key":{"type":"string"},"label":{"type":"string"},"points":{"type":"array","items":{"type":"object","properties":{"x":{},"y":{"type":"number"}}}}}}},` +
			`"rows":{"type":"array","description":"Rows read with xField, yField and seriesField (dotted paths) in place of series; a records.aggregate result's groups","items":{"type":"object"}},` +
			`"xField":{"type":"string","description":"The rows' x field (a dotted path such as key.stage); x when left out"},` +
			`"yField":{"type":"string","description":"The rows' y field (a dotted path such as values.amount); y when left out"},` +
			`"seriesField":{"type":"string","description":"The rows' field naming their series; one series when left out"},` +
			`"labels":{"type":"object","description":"x or series value → the label shown (stage ids to their names)"},` +
			`"order":{"type":"array","description":"The x values in the order to show them (a funnel's stages); first seen when left out","items":{}},` +
			`"format":{"type":"string","enum":["number","money","percent","duration"],"description":"number when left out; money values are in currency units, percents fractions (0.25), durations seconds"},` +
			`"currency":{"type":"string","description":"An ISO 4217 code for money, USD when left out"},` +
			`"stacked":{"type":"boolean","description":"Stack the series of a bar or area chart"},` +
			`"legend":{"type":"boolean","description":"Show the legend; true when there is more than one series"},` +
			`"height":{"type":"number","description":"Pixels, 240 when left out"},` +
			`"title":{"type":"string","description":"A caption above the chart, also its accessible name"},` + sx,
		events:  ev("select", "A bar, point or slice was clicked: payload {series, x}"),
		example: `{"type":"mui.chart","key":"byStage","props":{"kind":"bar","xField":"key.stage","yField":"values.amount","format":"money","currency":"USD","rows":[{"key":{"stage":"lead"},"values":{"amount":12000}},{"key":{"stage":"won"},"values":{"amount":5000}}]},"on":{"select":{"handler":"onStage","payload":{"stage":"event.x"}}}}`,
	})
	specs = append(specs, spec{id: "mui.metric", title: "Metric", category: "Display",
		tags: []string{"metric", "stat", "kpi", "number", "value", "delta", "dashboard", "aggregate"},
		description: "One number shown large and formatted (forge 40a), the Stat tile's value: as a number, money (with currency), a percent or a duration (seconds), with an optional compare value shown as the change from it (an arrow and a percent, green when up unless upIsBad). " +
			"A value may be a number or a decimal string, as records.aggregate gives them; it animates when it changes.",
		props: `"value":{"description":"The number (or decimal string)"},` +
			`"compare":{"description":"An earlier or target value; the change from it is shown"},` +
			`"compareLabel":{"type":"string","description":"What compare is, e.g. vs last month"},` +
			`"format":{"type":"string","enum":["number","money","percent","duration"],"description":"number when left out"},` +
			`"currency":{"type":"string","description":"An ISO 4217 code for money, USD when left out"},` +
			`"upIsBad":{"type":"boolean","description":"A rise is shown red (costs, open tickets)"},` +
			`"size":{"type":"string","enum":["small","medium","large"],"description":"medium when left out"},` + sx,
		example: `{"type":"mui.metric","key":"open","props":{"value":"125000.00","compare":100000,"format":"money","currency":"USD","compareLabel":"vs last month"}}`,
	})
}
