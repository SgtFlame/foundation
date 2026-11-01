package project

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

// MaxBatch is the most messages one atomic batch holds (the server's
// default limit).
const MaxBatch = 1000

// Error codes of an atomic batch refused by JetStream.
const (
	// ErrCodeBatchDuplicate: a message of the batch was stored before
	// under its id, so the batch was committed before.
	ErrCodeBatchDuplicate jetstream.ErrorCode = 10201
	// ErrCodeAtomicDisabled: the stream does not allow atomic batches.
	ErrCodeAtomicDisabled jetstream.ErrorCode = 10174
)

// BatchAck is a committed batch: the stream and its last message's
// sequence. The batch's messages are stored one after another.
type BatchAck struct {
	Stream string `json:"stream"`
	Seq    uint64 `json:"seq"`
	Count  int    `json:"count"`
}

type batchResponse struct {
	BatchAck
	Error *jetstream.APIError `json:"error"`
}

// PublishBatch publishes msgs to one stream as an atomic batch (NATS 2.12
// and later): every message is stored, or none is. Each message's expected
// headers (Nats-Msg-Id, Nats-Expected-Last-Subject-Sequence) are checked
// when the batch commits, and any that fails refuses the whole batch with
// a *jetstream.APIError. nats.go has no batch API yet, so this speaks the
// headers itself.
func PublishBatch(ctx context.Context, nc *nats.Conn, msgs []*nats.Msg) (BatchAck, error) {
	if len(msgs) > MaxBatch {
		return BatchAck{}, log.Errorf("a batch holds at most %d messages, not %d", MaxBatch, len(msgs))
	}
	id := nuid.Next()
	for i, m := range msgs {
		if m.Header == nil {
			m.Header = nats.Header{}
		}
		m.Header.Set("Nats-Batch-Id", id)
		m.Header.Set("Nats-Batch-Sequence", strconv.Itoa(i+1))
		if i == len(msgs)-1 {
			m.Header.Set("Nats-Batch-Commit", "1")
		}
		// Each message is acknowledged once staged (an empty reply), so a
		// refusal before the commit is seen where it happens
		resp, err := nc.RequestMsgWithContext(ctx, m)
		if err != nil {
			return BatchAck{}, log.Wrapf(err, "cannot publish message %d of a batch", i+1)
		}
		if len(resp.Data) == 0 {
			if i == len(msgs)-1 {
				return BatchAck{}, log.Errorf("the batch's commit got no acknowledgement")
			}
			continue
		}
		var r batchResponse
		if err := json.Unmarshal(resp.Data, &r); err != nil {
			return BatchAck{}, log.Wrapf(err, "cannot read a batch's acknowledgement")
		}
		if r.Error != nil {
			return BatchAck{}, r.Error
		}
		if i == len(msgs)-1 {
			return r.BatchAck, nil
		}
	}
	return BatchAck{}, log.Errorf("an empty batch")
}

// writeBatch puts several keys of a bucket as one atomic batch, each
// expecting the revision it was read at (0: it had none). A key changed
// meanwhile refuses the batch with jetstream.ErrKeyExists.
func writeBatch(ctx context.Context, nc *nats.Conn, bucket string, writes []kvWrite) error {
	msgs := make([]*nats.Msg, len(writes))
	for i, w := range writes {
		m := nats.NewMsg("$KV." + bucket + "." + w.key)
		m.Data = w.raw
		m.Header.Set(jetstream.ExpectedLastSubjSeqHeader, strconv.FormatUint(w.rev, 10))
		msgs[i] = m
	}
	_, err := PublishBatch(ctx, nc, msgs)
	if IsConflict(err) {
		return jetstream.ErrKeyExists
	}
	return err
}

type kvWrite struct {
	key string
	raw []byte
	rev uint64
}

// IsConflict reports whether err refused a publish because a subject's
// last sequence was not the one expected.
func IsConflict(err error) bool {
	var apiErr *jetstream.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence || apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequenceConstant
}
