package usagedto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/usage"
)

type UsageRecord struct {
	ID             uint64    `json:"id,string"`
	Time           time.Time `json:"time"`
	UserID         uint64    `json:"userId,string,omitempty"`
	Feature        string    `json:"feature"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	Level          string    `json:"level,omitempty"`
	ConversationID uint64    `json:"conversationId,string,omitempty"`
	RoundID        uint64    `json:"roundId,string,omitempty"`
	FileID         uint64    `json:"fileId,string,omitempty"`
	InputTokens    int       `json:"inputTokens"`
	CachedTokens   int       `json:"cachedTokens"`
	OutputTokens   int       `json:"outputTokens"`
	ThinkingTokens int       `json:"thinkingTokens"`
	EmbedTokens    int       `json:"embedTokens"`
	Estimated      bool      `json:"estimated,omitempty"`
	CostMicros     int64     `json:"costMicros"`
	Currency       string    `json:"currency,omitempty"`
	LatencyMs      int64     `json:"latencyMs"`
	Status         uint16    `json:"status"`
	Captured       bool      `json:"captured,omitempty"` // POST /usage/capture has its text, until it expires
}

func UsageRecordFrom(r usage.Record) UsageRecord {
	return UsageRecord{
		ID: r.ID, Time: r.CreatedAt, UserID: r.UserID, Feature: r.Feature, Provider: r.Provider, Model: r.ModelName, Level: r.Level,
		ConversationID: r.ConversationID, RoundID: r.RoundID, FileID: r.FileID,
		InputTokens: r.InputTokens, CachedTokens: r.CachedTokens, OutputTokens: r.OutputTokens, ThinkingTokens: r.ThinkingTokens,
		EmbedTokens: r.EmbedTokens, Estimated: r.Estimated, CostMicros: r.CostMicros, Currency: r.Currency,
		LatencyMs: r.LatencyMs, Status: r.Status, Captured: r.Captured,
	}
}
