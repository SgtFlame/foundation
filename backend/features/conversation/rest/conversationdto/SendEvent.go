package conversationdto

import "encoding/json"

// The Server-Sent Events of POST /conversation/send: "started", "sources"
// when the round injected citable context (again whenever a tool result
// adds citations), then "delta" and "tool" (a ToolCall) events, "done",
// possibly "title", and a "followed" event per round follower that did
// something; or "error" (a chatdto.StreamError) when the stream fails.

// SendStarted is the data of a "started" event.
type SendStarted struct {
	Conversation Conversation `json:"conversation"`
	Round        Round        `json:"round"`
}

// SendSources is the data of a "sources" event: the segments the round
// injected, [1] first.
type SendSources struct {
	Segments []RoundSegment `json:"segments"`
}

// SendDelta is the data of a "delta" event.
type SendDelta struct {
	Text string `json:"text"`
}

// SendDone is the data of a "done" event: the completed round, whose status
// says whether the reply failed.
type SendDone struct {
	Round Round `json:"round"`
}

// SendTitle is the data of a "title" event.
type SendTitle struct {
	Conversation Conversation `json:"conversation"`
}

// SendFollowed is the data of a "followed" event: what a chat.roundFollower
// did after the reply, such as Chronicle's memory update.
type SendFollowed struct {
	Follower string          `json:"follower"`
	Summary  string          `json:"summary"`
	Data     json.RawMessage `json:"data,omitempty"`
}
