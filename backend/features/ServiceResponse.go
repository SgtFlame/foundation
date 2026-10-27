package features

type ServiceResponse struct {
	Code      uint16 // an HTTP status
	Message   string // safe to show users
	ErrorCode string // the oops code, e.g. "volume_not_found", for clients to match on
	Trace     string // the request's trace ID; for the server only, never serialized
}
