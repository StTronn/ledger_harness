// Package seed is the deterministic seeder (SPEC §2 Phase 2, §4.4): from seeded
// synthetic rules — NO live Razorpay — it generates one internally-consistent
// month of DTC activity (Razorpay-shaped payments/refunds/settlements/disputes
// plus an independent bank feed; these are the AGENT INPUTS) together with the
// matching hidden ground-truth ledger (truth/gl.json; SCORER ONLY), built from
// the same generation rules.
//
// # Determinism (SPEC §2, §12)
//
// Everything is driven by a math/rand stream seeded from a stable hash of
// (world, period) — see SeedFor. There is NO wall-clock, NO crypto randomness,
// NO map iteration leaking into output: the same (world, period) yields the same
// stream and therefore byte-identical fixtures and truth GL. A test runs the
// seeder twice and diffs the bytes.
//
// # Money invariant (SPEC §1, §4)
//
// All amounts are integer minor units — paise — as internal/money.Money or raw
// int64 paise. No floating-point type appears anywhere in this package (a guard
// test asserts that statically), so GST splits and fee/round arithmetic stay
// exact.
//
// # truth/ isolation (SPEC §4.4, §12)
//
// The seeder is one of the few packages allowed to touch internal/truth: it
// WRITES truth/gl.json. ingest/normalize/classify/reconcile/agents must not.
package seed

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Layout resolves every artifact path under a worlds/<world>/<period>/ tree
// (SPEC §4.4). It is the single source of "where files go", so the writer, the
// reader (later phases), and the tests all agree on layout. Root is the base
// directory that contains the worlds/ tree (the repo root in normal use); paths
// are joined with filepath.Join so they are correct on any OS.
type Layout struct {
	Root   string // base dir containing worlds/ (e.g. the repo root)
	World  string // world name (e.g. "dtc")
	Period string // accounting period as YYYY-MM (e.g. "2026-05")
}

// NewLayout builds a Layout after validating world and period. World must be a
// non-empty simple token (no path separators); period must be a strict YYYY-MM.
// Validating here means every downstream path is well-formed and no caller can
// inject "../" traversal through world/period.
func NewLayout(root, world, period string) (Layout, error) {
	if root == "" {
		return Layout{}, fmt.Errorf("seed: layout root is empty")
	}
	if err := validateWorld(world); err != nil {
		return Layout{}, err
	}
	if err := ValidatePeriod(period); err != nil {
		return Layout{}, err
	}
	return Layout{Root: root, World: world, Period: period}, nil
}

// validateWorld checks the world is a non-empty token with no path separators or
// traversal, so it is safe to embed in a filesystem path.
func validateWorld(world string) error {
	if world == "" {
		return fmt.Errorf("seed: world is empty")
	}
	if strings.ContainsAny(world, `/\`) || world == "." || world == ".." {
		return fmt.Errorf("seed: world %q must be a simple name (no path separators)", world)
	}
	return nil
}

// ValidatePeriod checks period is exactly YYYY-MM with a 1..12 month. It is
// exported because the CLI validates the flag before constructing a Layout, and
// later phases (close/report/diff) reuse the same rule so the period format is
// defined once.
func ValidatePeriod(period string) error {
	// Expected shape: 4 digits, '-', 2 digits.
	if len(period) != 7 || period[4] != '-' {
		return fmt.Errorf("seed: period %q must be YYYY-MM", period)
	}
	year := period[:4]
	month := period[5:]
	if !allDigits(year) || !allDigits(month) {
		return fmt.Errorf("seed: period %q must be YYYY-MM (digits only)", period)
	}
	// Month must be 01..12. Parse without strconv's float paths.
	m := int(month[0]-'0')*10 + int(month[1]-'0')
	if m < 1 || m > 12 {
		return fmt.Errorf("seed: period %q has invalid month %q (want 01..12)", period, month)
	}
	return nil
}

// allDigits reports whether every byte of s is an ASCII decimal digit (and s is
// non-empty).
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// PeriodDir is worlds/<world>/<period>/ — the directory that holds all of this
// period's artifacts.
func (l Layout) PeriodDir() string {
	return filepath.Join(l.Root, "worlds", l.World, l.Period)
}

// RazorpayDir is worlds/<world>/<period>/razorpay/ — the directory holding the
// Razorpay-shaped fixture files (agent input).
func (l Layout) RazorpayDir() string {
	return filepath.Join(l.PeriodDir(), "razorpay")
}

// TruthDir is worlds/<world>/<period>/truth/ — the directory holding the hidden
// ground-truth GL (SCORER ONLY).
func (l Layout) TruthDir() string {
	return filepath.Join(l.PeriodDir(), "truth")
}

// PaymentsPath, RefundsPath, SettlementsPath, DisputesPath, OrdersPath are the
// Razorpay fixture files under razorpay/ (SPEC §4.4, §2). Orders are an
// agent-input recovery source (the agent "fetches the order" for missing tax
// metadata), not an accounting event — ingest/normalize never read orders.json.
func (l Layout) PaymentsPath() string    { return filepath.Join(l.RazorpayDir(), "payments.json") }
func (l Layout) RefundsPath() string     { return filepath.Join(l.RazorpayDir(), "refunds.json") }
func (l Layout) SettlementsPath() string { return filepath.Join(l.RazorpayDir(), "settlements.json") }
func (l Layout) DisputesPath() string    { return filepath.Join(l.RazorpayDir(), "disputes.json") }
func (l Layout) OrdersPath() string      { return filepath.Join(l.RazorpayDir(), "orders.json") }

// BankFeedPath is worlds/<world>/<period>/bank-feed.json — the independent bank
// record (agent input).
func (l Layout) BankFeedPath() string {
	return filepath.Join(l.PeriodDir(), "bank-feed.json")
}

// TruthGLPath is worlds/<world>/<period>/truth/gl.json — the hidden ground-truth
// ledger (SCORER ONLY).
func (l Layout) TruthGLPath() string {
	return filepath.Join(l.TruthDir(), "gl.json")
}
