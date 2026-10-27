package impl

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
)

type fakeProvider struct{ got chatservice.ChatInput }

func (f *fakeProvider) ChatModel() string { return "fake" }

func (f *fakeProvider) Complete(ctx context.Context, in chatservice.ChatInput) (chatservice.ChatOutput, error) {
	return f.Stream(ctx, in, func(chatservice.Delta) error { return nil })
}

func (f *fakeProvider) Stream(_ context.Context, in chatservice.ChatInput, onDelta func(chatservice.Delta) error) (chatservice.ChatOutput, error) {
	f.got = in
	words := strings.Fields(in.Messages[len(in.Messages)-1].Content)
	for _, w := range words {
		if err := onDelta(chatservice.Delta{Text: w}); err != nil {
			return chatservice.ChatOutput{}, err
		}
	}
	return chatservice.ChatOutput{Content: strings.Join(words, ""), FinishReason: "stop"}, nil
}

func TestStream(t *testing.T) {
	p := &fakeProvider{}
	s := newService(WithProvider(p))
	req := s.Stream(chatservice.StreamArgs{System: "be brief", Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: "a b c"}}})
	var got string
	for d := range req.Deltas {
		got += d.Text
	}
	resp := <-req.Response
	if resp.Common.Code != http.StatusOK || got != "abc" || resp.Message.Content != "abc" || resp.Model != "fake" || p.got.System != "be brief" {
		t.Fatalf("got %q, %+v", got, resp)
	}
}

func TestCompleteValidates(t *testing.T) {
	s := newService(WithProvider(&fakeProvider{}))
	for name, msgs := range map[string][]chatservice.Message{
		"empty":          nil,
		"bad role":       {{Role: "system", Content: "x"}},
		"assistant last": {{Role: chatservice.RoleUser, Content: "x"}, {Role: chatservice.RoleAssistant, Content: "y"}},
	} {
		if code := s.Complete(chatservice.CompleteArgs{Messages: msgs}).Common.Code; code != http.StatusBadRequest {
			t.Errorf("%s: code %d", name, code)
		}
	}
	if code := newService().Complete(chatservice.CompleteArgs{}).Common.Code; code != http.StatusServiceUnavailable {
		t.Errorf("no provider: code %d", code)
	}
}
