package closer_test

import (
	"encoding/json"
	"testing"
)

// blankFirstGSTRate decodes a payments.json array, removes the gst_rate from the
// FIRST payment's notes (simulating absent tax metadata), and re-marshals the
// array. It returns the re-encoded bytes and the id of the payment it edited.
//
// It works on a generic map representation so every other field — and the order
// of the other payments — is preserved; only the one notes.gst_rate is dropped,
// which is exactly the "missing metadata" condition the classifier flags.
func blankFirstGSTRate(t *testing.T, data []byte) ([]byte, string) {
	t.Helper()
	var payments []map[string]any
	if err := json.Unmarshal(data, &payments); err != nil {
		t.Fatalf("unmarshal payments: %v", err)
	}
	if len(payments) == 0 {
		t.Fatal("no payments to strip")
	}

	first := payments[0]
	id, _ := first["id"].(string)
	if id == "" {
		t.Fatal("first payment has no id")
	}
	notes, ok := first["notes"].(map[string]any)
	if !ok {
		t.Fatal("first payment has no notes object")
	}
	delete(notes, "gst_rate")

	out, err := json.Marshal(payments)
	if err != nil {
		t.Fatalf("marshal payments: %v", err)
	}
	return out, id
}
