package configdto

// ChangedEvent is the data of a "changed" Server-Sent Event on /config/events.
type ChangedEvent struct {
	Section string `json:"section"`
	Scope   string `json:"scope"`
}
