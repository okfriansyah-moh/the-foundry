package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Anchor struct {
	StartSeq  int64
	EndSeq    int64
	Digest    string
	CreatedAt time.Time
}

func AnchorDigest(rows []Row) string {
	h := sha256.New()
	for _, row := range rows {
		h.Write(row.Hash)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func BuildAnchors(rows []Row, stride int, now time.Time) []Anchor {
	if stride <= 0 {
		stride = 10000
	}
	var anchors []Anchor
	for i := 0; i < len(rows); i += stride {
		end := i + stride
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		anchors = append(anchors, Anchor{StartSeq: chunk[0].Seq, EndSeq: chunk[len(chunk)-1].Seq, Digest: AnchorDigest(chunk), CreatedAt: now.UTC()})
	}
	return anchors
}
