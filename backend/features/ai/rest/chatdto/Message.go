package chatdto

import "github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"

type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

func (m Message) ToMessage() chatservice.Message {
	return chatservice.Message{Role: chatservice.Role(m.Role), Content: m.Content}
}

func MessageFrom(m chatservice.Message) Message {
	return Message{Role: string(m.Role), Content: m.Content}
}
