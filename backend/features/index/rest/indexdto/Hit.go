package indexdto

import (
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/kb/rest/kbdto"
)

type Hit struct {
	VolumeID uint64           `json:"volumeId,string"`
	Path     kbdto.VolumePath `json:"path"` // folders down to the file's name, without its extension
	Type     string           `json:"type"`
	Score    float64          `json:"score"`
	Chunks   []ChunkRef       `json:"chunks"`
}

type ChunkRef struct {
	Start       int     `json:"start"` // byte offsets in the file
	End         int     `json:"end"`
	StartLine   int     `json:"startLine"`
	EndLine     int     `json:"endLine"`
	Heading     string  `json:"heading,omitempty"`
	Head        string  `json:"head"`
	Tail        string  `json:"tail"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	VectorRank  int     `json:"vectorRank,omitempty"`
	KeywordRank int     `json:"keywordRank,omitempty"`
}

func HitFrom(h indexservice.Hit) Hit {
	chunks := make([]ChunkRef, len(h.Chunks))
	for i, c := range h.Chunks {
		chunks[i] = ChunkRef{
			Start: c.Start, End: c.End, StartLine: c.StartLine, EndLine: c.EndLine,
			Heading: c.Heading, Head: c.Head, Tail: c.Tail, Snippet: c.Snippet,
			Score: c.Score, VectorRank: c.VectorRank, KeywordRank: c.KeywordRank,
		}
	}
	return Hit{VolumeID: h.VolumeID, Path: kbdto.VolumePath{Folders: h.Folders}, Type: h.Type, Score: h.Score, Chunks: chunks}
}
