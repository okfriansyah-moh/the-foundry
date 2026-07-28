package audit

import (
	"testing"
	"time"
)

func TestBuildAnchors(t *testing.T) {
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	anchors := BuildAnchors([]Row{{Seq: 1, Hash: []byte("a")}, {Seq: 2, Hash: []byte("b")}}, 1, now)
	if len(anchors) != 2 || anchors[0].StartSeq != 1 || anchors[1].EndSeq != 2 {
		t.Fatalf("anchors=%+v", anchors)
	}
}
