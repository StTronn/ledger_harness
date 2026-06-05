package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// canonicalPlaybookPath is the repo-root config/playbook.json — the single
// source of truth the embedded copy must mirror exactly.
func canonicalPlaybookPath() string {
	return filepath.Join("..", "..", "config", "playbook.json")
}

// TestEmbeddedPlaybookMatchesCanonical is the drift guard: the bytes baked into
// the binary (playbook.embed.json) MUST equal the canonical config/playbook.json.
// If they diverge — e.g. someone edits one but not the other — this fails the
// build, keeping the embed a verified mirror of the single source of truth
// (SPEC §6, §13). The fix when it fails is to re-copy the canonical file:
//
//	cp config/playbook.json internal/config/playbook.embed.json
func TestEmbeddedPlaybookMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile(canonicalPlaybookPath())
	if err != nil {
		t.Fatalf("read canonical playbook: %v", err)
	}
	if !bytes.Equal(canonical, embeddedPlaybook) {
		t.Errorf("embedded playbook (%d bytes) differs from canonical config/playbook.json (%d bytes); "+
			"re-copy with: cp config/playbook.json internal/config/playbook.embed.json",
			len(embeddedPlaybook), len(canonical))
	}
}

// TestDefaultPlaybookParsesAndValidates asserts the embedded default playbook
// loads through the same validation as Load, and exposes the four v1 entry types
// and the chart accounts the seeder binds against.
func TestDefaultPlaybookParsesAndValidates(t *testing.T) {
	pb, err := DefaultPlaybook()
	if err != nil {
		t.Fatalf("DefaultPlaybook: %v", err)
	}
	for _, name := range []string{"dtc_sale", "razorpay_settlement", "refund_reversal", "chargeback_loss"} {
		if _, ok := pb.EntryType(name); !ok {
			t.Errorf("default playbook missing entry type %q", name)
		}
	}
	for _, path := range []string{
		"assets/bank",
		"assets/razorpay-settlement-receivable",
		"liabilities/gst-output-payable",
		"income/product-sales",
		"income/sales-returns",
		"expense/processor-fees",
		"expense/gst-input",
		"expense/chargeback-loss",
	} {
		if _, ok := pb.Account(path); !ok {
			t.Errorf("default playbook missing account %q", path)
		}
	}

	// DefaultPlaybook must equal the canonical file when both are parsed, so a
	// caller using the embed gets the same chart/templates as one using Load.
	loaded, err := Load(canonicalPlaybookPath())
	if err != nil {
		t.Fatalf("Load canonical: %v", err)
	}
	if len(loaded.Accounts) != len(pb.Accounts) || len(loaded.EntryTypes) != len(pb.EntryTypes) {
		t.Errorf("embedded vs loaded shape differs: accounts %d/%d, entry types %d/%d",
			len(pb.Accounts), len(loaded.Accounts), len(pb.EntryTypes), len(loaded.EntryTypes))
	}
}
