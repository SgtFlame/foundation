package usage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/usage/settings"
)

type fakeChat struct{ err error }

func (fakeChat) ChatModel() string { return "m" }
func (f fakeChat) Complete(context.Context, chatservice.ChatInput) (chatservice.ChatOutput, error) {
	return chatservice.ChatOutput{Usage: chatservice.Usage{InputTokens: 1000, CachedTokens: 400, OutputTokens: 100, ThinkingTokens: 20}}, f.err
}
func (f fakeChat) Stream(ctx context.Context, in chatservice.ChatInput, _ func(chatservice.Delta) error) (chatservice.ChatOutput, error) {
	return f.Complete(ctx, in)
}

type fakeEmbed struct{}

func (fakeEmbed) EmbedModel() embedservice.ModelInfo { return embedservice.ModelInfo{Model: "e"} }
func (fakeEmbed) Embed(_ context.Context, in embedservice.EmbedInput) (embedservice.EmbedOutput, error) {
	return embedservice.EmbedOutput{Vectors: make([][]float32, len(in.Texts))}, nil
}

func TestRecordsArePricedAndAttributed(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, Models()...); err != nil {
		t.Fatal(err)
	}
	prices := func(context.Context) (aisettings.Models, error) {
		return aisettings.Models{Models: []aisettings.Model{
			{Provider: "fake", Model: "m", Pricing: aisettings.Pricing{Currency: "USD", InputPerMTok: 1, CachedInputPerMTok: 0.5, OutputPerMTok: 10}},
			{Provider: "fake", Model: "e", Pricing: aisettings.Pricing{EmbedPerMTok: 2}},
		}}, nil
	}
	events := NewEvents()
	published := 0
	events.Subscribe(func(Record) { published++ })
	r := NewRecorder(gdb, prices, events, nil)

	args := features.ServiceArgs{Principal: features.LocalPrincipal(), Meta: features.Meta{Level: "fast", ConversationID: 7}}
	chat := ChatProvider(fakeChat{}, "fake", r)
	if _, err := chat.Complete(args.CallContext(), chatservice.ChatInput{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	failing := ChatProvider(fakeChat{err: providers.Errorf(429, "slow down")}, "fake", r)
	_, _ = failing.Stream(args.CallContext(), chatservice.ChatInput{Model: "m"}, nil)
	embed := EmbedProvider(fakeEmbed{}, "fake", r)
	indexArgs := features.BackgroundArgs(context.Background())
	indexArgs.Meta = features.Meta{Feature: "embed-index"}
	if _, err := embed.Embed(indexArgs.CallContext(), embedservice.EmbedInput{Model: "e", Texts: []string{"12345678"}}); err != nil {
		t.Fatal(err)
	}
	r.Close()

	var recs []Record
	if err := gdb.Order("id").Find(&recs).Error; err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || published != 3 {
		t.Fatalf("%d records, %d published", len(recs), published)
	}
	c := recs[0]
	if c.Feature != "chat" || c.Level != "fast" || c.ConversationID != 7 || c.UserID != features.LocalUserID || c.TenantID != features.LocalTenantID ||
		c.CostMicros != 600*1+400*0.5+120*10 || c.Currency != "USD" || c.Status != 200 {
		t.Errorf("chat record %+v", c)
	}
	if recs[1].Status != 429 {
		t.Errorf("failed call status %d", recs[1].Status)
	}
	e := recs[2]
	if e.Feature != "embed-index" || e.UserID != 0 || !e.Estimated || e.EmbedTokens != 2 || e.CostMicros != 4 {
		t.Errorf("embed record %+v", e)
	}
}

func TestCapturedCallsKeepTheirText(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, Models()...); err != nil {
		t.Fatal(err)
	}
	// An old capture from before, already past the retention
	old := Capture{RecordID: 1, Reply: "old"}
	if err := gdb.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&old).UpdateColumn("created_at", time.Now().Add(-48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	on := true
	r := NewRecorder(gdb, nil, nil, func(context.Context) (usagesettings.Capture, error) {
		return usagesettings.Capture{Enabled: on, RetentionHours: 24}, nil
	})
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	chat := ChatProvider(fakeChat{}, "fake", r)
	in := chatservice.ChatInput{Model: "m", System: "be brief", Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: "hi"}}}
	if _, err := chat.Complete(args.CallContext(), in); err != nil {
		t.Fatal(err)
	}
	on = false
	if _, err := chat.Complete(args.CallContext(), in); err != nil {
		t.Fatal(err)
	}
	r.Close()

	var recs []Record
	gdb.Order("id").Find(&recs)
	var caps []Capture
	gdb.Find(&caps)
	if len(recs) != 2 || !recs[0].Captured || recs[1].Captured {
		t.Fatalf("records %+v", recs)
	}
	if len(caps) != 1 || caps[0].RecordID != recs[0].ID || caps[0].System != "be brief" ||
		caps[0].Messages != `[{"role":"user","content":"hi"}]` || caps[0].TenantID != features.LocalTenantID {
		t.Fatalf("captures %+v (the expired one should be purged)", caps)
	}
}
