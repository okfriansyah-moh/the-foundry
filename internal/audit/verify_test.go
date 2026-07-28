package audit

import "testing"

func TestVerifyRows_TamperDetected(t *testing.T) {
	rows := []Row{{Seq: 1, Payload: []byte(`{"ok":true}`), PrevHash: nil, Hash: []byte("bad")}}
	result := VerifyRows(rows)
	if result.OK || result.BadSeq != 1 {
		t.Fatalf("result=%+v", result)
	}
}
