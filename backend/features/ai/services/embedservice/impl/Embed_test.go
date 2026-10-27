package impl

import (
	"context"
	"net/http"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
)

type fakeProvider struct {
	calls [][]string
	err   error
}

func (f *fakeProvider) EmbedModel() embedservice.ModelInfo {
	return embedservice.ModelInfo{Model: "fake", Dimensions: 2, MaxBatch: 3}
}

func (f *fakeProvider) Embed(_ context.Context, in embedservice.EmbedInput) (embedservice.EmbedOutput, error) {
	if f.err != nil {
		return embedservice.EmbedOutput{}, f.err
	}
	f.calls = append(f.calls, in.Texts)
	out := embedservice.EmbedOutput{Usage: embedservice.Usage{InputTokens: len(in.Texts)}}
	for i := range in.Texts {
		out.Vectors = append(out.Vectors, []float32{float32(len(f.calls)), float32(i)})
	}
	return out, nil
}

func TestEmbedBatches(t *testing.T) {
	p := &fakeProvider{}
	s := newService(WithProvider(p), WithBatchSize(10))
	resp := s.Embed(embedservice.EmbedArgs{Texts: []string{"a", "b", "c", "d", "e"}})
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("%d %s", resp.Common.Code, resp.Common.Message)
	}
	if len(p.calls) != 2 || len(p.calls[0]) != 3 || len(p.calls[1]) != 2 {
		t.Fatalf("calls %v, want batches of the provider's 3", p.calls)
	}
	if len(resp.Vectors) != 5 || resp.Vectors[4][0] != 2 || resp.Dimensions != 2 || resp.Usage.InputTokens != 5 || resp.Model != "fake" {
		t.Fatalf("response %+v", resp)
	}
}

func TestEmbedErrors(t *testing.T) {
	cases := []struct {
		name string
		s    *service
		args embedservice.EmbedArgs
		code uint16
	}{
		{"no provider", newService(), embedservice.EmbedArgs{Texts: []string{"a"}}, http.StatusServiceUnavailable},
		{"no texts", newService(WithProvider(&fakeProvider{})), embedservice.EmbedArgs{}, http.StatusBadRequest},
		{"blank text", newService(WithProvider(&fakeProvider{})), embedservice.EmbedArgs{Texts: []string{" "}}, http.StatusBadRequest},
		{"bad purpose", newService(WithProvider(&fakeProvider{})), embedservice.EmbedArgs{Texts: []string{"a"}, Purpose: "x"}, http.StatusBadRequest},
		{"rate limited", newService(WithProvider(&fakeProvider{err: providers.Errorf(http.StatusTooManyRequests, "slow down")})), embedservice.EmbedArgs{Texts: []string{"a"}}, http.StatusTooManyRequests},
		{"upstream", newService(WithProvider(&fakeProvider{err: context.DeadlineExceeded})), embedservice.EmbedArgs{Texts: []string{"a"}}, http.StatusBadGateway},
	}
	for _, c := range cases {
		if got := c.s.Embed(c.args).Common.Code; got != c.code {
			t.Errorf("%s: code %d, want %d", c.name, got, c.code)
		}
	}
}
