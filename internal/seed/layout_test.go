package seed

import (
	"path/filepath"
	"testing"
)

// TestValidatePeriod is table-driven over the YYYY-MM format rule.
func TestValidatePeriod(t *testing.T) {
	tests := []struct {
		period string
		ok     bool
	}{
		{"2026-05", true},
		{"2026-01", true},
		{"2026-12", true},
		{"2026-00", false}, // month 0
		{"2026-13", false}, // month 13
		{"2026-5", false},  // not zero-padded / wrong length
		{"202605", false},  // missing dash
		{"abcd-05", false}, // non-digit year
		{"2026-0a", false}, // non-digit month
		{"", false},
		{"2026-05-01", false}, // too long
	}
	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			err := ValidatePeriod(tt.period)
			if tt.ok && err != nil {
				t.Errorf("ValidatePeriod(%q) = %v, want nil", tt.period, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("ValidatePeriod(%q) = nil, want error", tt.period)
			}
		})
	}
}

// TestNewLayoutValidation asserts world/period validation rejects unsafe inputs.
func TestNewLayoutValidation(t *testing.T) {
	tests := []struct {
		name   string
		root   string
		world  string
		period string
		ok     bool
	}{
		{"good", "/tmp", "dtc", "2026-05", true},
		{"empty root", "", "dtc", "2026-05", false},
		{"empty world", "/tmp", "", "2026-05", false},
		{"world with slash", "/tmp", "a/b", "2026-05", false},
		{"world traversal", "/tmp", "..", "2026-05", false},
		{"bad period", "/tmp", "dtc", "2026-99", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLayout(tt.root, tt.world, tt.period)
			if tt.ok && err != nil {
				t.Errorf("NewLayout = %v, want nil", err)
			}
			if !tt.ok && err == nil {
				t.Errorf("NewLayout = nil, want error")
			}
		})
	}
}

// TestLayoutPaths asserts the artifact paths match the SPEC §4.4 layout exactly.
func TestLayoutPaths(t *testing.T) {
	l, err := NewLayout("/root", "dtc", "2026-05")
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	base := filepath.Join("/root", "worlds", "dtc", "2026-05")
	cases := map[string]string{
		l.PeriodDir():       base,
		l.RazorpayDir():     filepath.Join(base, "razorpay"),
		l.TruthDir():        filepath.Join(base, "truth"),
		l.PaymentsPath():    filepath.Join(base, "razorpay", "payments.json"),
		l.RefundsPath():     filepath.Join(base, "razorpay", "refunds.json"),
		l.SettlementsPath(): filepath.Join(base, "razorpay", "settlements.json"),
		l.DisputesPath():    filepath.Join(base, "razorpay", "disputes.json"),
		l.BankFeedPath():    filepath.Join(base, "bank-feed.json"),
		l.TruthGLPath():     filepath.Join(base, "truth", "gl.json"),
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
	}
}
