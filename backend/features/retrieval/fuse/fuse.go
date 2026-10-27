// Package fuse is the one implementation of weighted Reciprocal Rank Fusion,
// shared by indexservice.Find and the retrieval engine
// (documentation/plans/ContextRetrieval.md).
package fuse

import "sort"

// DefaultK damps the head of each ranking; 60 is the customary value.
const DefaultK = 60

// Ranked is one item with its fused score and its 1-based rank in each
// ranking, 0 when that ranking did not find it.
type Ranked[T any] struct {
	Item                    T
	Score                   float64
	VectorRank, KeywordRank int
}

// Fuse merges the two rankings by weighted Reciprocal Rank Fusion: an item
// scores vectorWeight/(k+vectorRank) + (1-vectorWeight)/(k+keywordRank), and
// a ranking that misses it adds nothing. Ranks, unlike raw scores, are
// comparable between cosine distance and BM25, and an item both searches find
// beats one only a single search found at the same rank. Items are the same
// when key says so; the first one seen is kept. The result is best first.
func Fuse[T any, K comparable](vector, keyword []T, key func(T) K, vectorWeight float64, k int) []Ranked[T] {
	if k <= 0 {
		k = DefaultK
	}
	byKey := map[K]*Ranked[T]{}
	var order []*Ranked[T]
	add := func(items []T, w float64, setRank func(*Ranked[T], int)) {
		for i, item := range items {
			id := key(item)
			r, ok := byKey[id]
			if !ok {
				r = &Ranked[T]{Item: item}
				byKey[id] = r
				order = append(order, r)
			}
			setRank(r, i+1)
			r.Score += w / float64(k+i+1)
		}
	}
	add(vector, vectorWeight, func(r *Ranked[T], rank int) { r.VectorRank = rank })
	add(keyword, 1-vectorWeight, func(r *Ranked[T], rank int) { r.KeywordRank = rank })

	out := make([]Ranked[T], len(order))
	for i, r := range order {
		out[i] = *r
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
