package chatdto

// StreamDelta is the data of a "delta" Server-Sent Event; the stream ends with
// a "done" event carrying a CompleteResponse, or an "error" event carrying one
// with only a message and code.
type StreamDelta struct {
	Text string `json:"text"`
}

// StreamError is the data of an "error" event: the status the request would
// have had, its message and its error code.
type StreamError struct {
	Code      uint16 `json:"code"`
	Message   string `json:"message"`
	ErrorCode string `json:"errorCode,omitempty"`
}
