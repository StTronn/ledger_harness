package seed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/razorpay/ledger-flow/internal/truth"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// Seed is the top-level seeder entry point used by the CLI (SPEC §10
// `ledger-flow seed`): it generates the substrate for (world, period) and writes
// every artifact under root/worlds/<world>/<period>/ (SPEC §4.4). It returns the
// Result so the caller can report what was written. Re-running with the same
// (world, period) overwrites the files with byte-identical content (the
// generator is deterministic and the writer marshals stably), which is exactly
// the reproducibility the gate asserts.
func Seed(root, world, period string) (Result, error) {
	return SeedWith(root, world, period, Options{})
}

// SeedWith is Seed plus the Options knobs (SPEC §1, §2, §5, §7): it generates and
// writes the substrate for (world, period), optionally seeding a reconciliation
// break (opts.Inject) and/or the missing-metadata ambiguity long tail
// (opts.Ambiguity) into the agent-input fixtures. Both are post-generation
// transforms, so the clean (no-knobs) path is byte-identical to Seed and the
// committed clean fixtures are unaffected. The returned Result carries the
// InjectResult and AmbiguityResult so the CLI can report what was perturbed.
func SeedWith(root, world, period string, opts Options) (Result, error) {
	layout, err := NewLayout(root, world, period)
	if err != nil {
		return Result{}, err
	}

	fx, feed, gl, res, err := GenerateFull(world, period, opts)
	if err != nil {
		return Result{}, err
	}

	if err := writeAll(layout, fx, feed, gl); err != nil {
		return Result{}, err
	}

	return Result{
		Layout:         layout,
		NumPayments:    len(fx.Payments),
		NumRefunds:     len(fx.Refunds),
		NumSettlements: len(fx.Settlements),
		NumDisputes:    len(fx.Disputes),
		NumOrders:      len(fx.Orders),
		NumGLEntries:   len(gl.Entries),
		BankCredits:    len(feed.Credits),
		BankDebits:     len(feed.Debits),
		Inject:         res.Inject,
		Ambiguity:      res.Ambiguity,
		Partial:        res.Partial,
	}, nil
}

// Result summarizes a completed seeding run for human-readable CLI output. It
// holds counts only; the substrate itself lives on disk under Layout.
type Result struct {
	Layout         Layout
	NumPayments    int
	NumRefunds     int
	NumSettlements int
	NumDisputes    int
	NumOrders      int
	NumGLEntries   int
	BankCredits    int
	BankDebits     int
	Inject         InjectResult    // what break (if any) was seeded; zero value = clean
	Ambiguity      AmbiguityResult // missing-metadata long tail seeded; zero value = none
	Partial        PartialResult   // partial-refund designations; zero value = none
}

// writeAll creates the directory tree and writes the six artifact files. The
// Razorpay fixtures and bank feed go under razorpay/ and the period dir; the
// truth GL goes under truth/ (the SCORER-ONLY isolation boundary, SPEC §4.4).
func writeAll(l Layout, fx Fixtures, feed BankFeed, gl truth.GL) error {
	if err := os.MkdirAll(l.RazorpayDir(), 0o755); err != nil {
		return fmt.Errorf("seed: create razorpay dir: %w", err)
	}
	if err := os.MkdirAll(l.TruthDir(), 0o755); err != nil {
		return fmt.Errorf("seed: create truth dir: %w", err)
	}

	// The agent-input fixtures are written by the seeder's own stable writer.
	// orders.json is the authoritative tax-metadata recovery source (SPEC §2); it
	// is an agent input, not an accounting event, so it lives under razorpay/ but is
	// never normalized into the event journal.
	writes := []struct {
		path string
		v    any
	}{
		{l.PaymentsPath(), fx.Payments},
		{l.RefundsPath(), fx.Refunds},
		{l.SettlementsPath(), fx.Settlements},
		{l.DisputesPath(), fx.Disputes},
		{l.OrdersPath(), fx.Orders},
		{l.BankFeedPath(), feed},
		// ratecard.json is the merchant's CONTRACTED fee schedule — the validation
		// table the fee-tier policy checks settlements against. It is emitted from
		// the SAME constants the generator prices fees with (feeBps,
		// razorpayGSTRate), so on a clean period expected == actual by
		// construction; a future mis-booked-fee inject perturbs the fixtures, not
		// this card.
		{l.RateCardPath(), rateCardFor(fx)},
	}
	for _, w := range writes {
		if err := writeJSONFile(w.path, w.v); err != nil {
			return err
		}
	}

	// The COD rail emits its own feed (cod.go). Written only for COD periods, so a
	// Razorpay-only period has no courier-feed.json and ingest reads none.
	if fx.Courier != nil {
		if err := writeJSONFile(l.CourierFeedPath(), fx.Courier); err != nil {
			return err
		}
	}

	// The ground-truth GL is written through internal/truth — the SINGLE package
	// allowed to read or write truth/gl.json (SPEC §4.4, §12). truth.WriteTruth
	// produces byte-identical output to the seeder's stable writer (same indent,
	// no HTML escaping, trailing newline) and writes atomically, so routing it
	// here keeps the isolation boundary clean without changing the on-disk bytes.
	if err := truth.WriteTruth(l.TruthGLPath(), gl); err != nil {
		return err
	}
	return nil
}

// rateCardFor builds the merchant's contracted fee schedule for a period. It is
// emitted from the SAME constants the generator prices fees with, so expected ==
// actual by construction. The razorpay channel is always present; a COD period
// (fx.Courier set) adds the courier channel with its flat COD collection fee and
// RTO fee, the table the COD fee/RTO policies validate deductions against.
func rateCardFor(fx Fixtures) feeds.RateCardFile {
	rc := feeds.RateCardFile{
		SchemaVersion: 1,
		Channels: []feeds.Channel{
			{Channel: "razorpay", FeeBps: feeBps, FeeGSTRate: razorpayGSTRate},
		},
	}
	if fx.Courier != nil {
		rc.Channels = append(rc.Channels, feeds.Channel{
			Channel:     codCourierChannel,
			FeeGSTRate:  razorpayGSTRate,
			CODFeePaise: codCollectionFeePaise,
			RTOFeePaise: rtoFeeNetPaise,
		})
	}
	return rc
}

// writeJSONFile marshals v to stable, indented JSON and writes it atomically to
// path. "Stable" means: encoding/json emits struct fields in declaration order
// and slices in order (and the generator emits no maps into output), so the
// bytes are reproducible across runs. A trailing newline is added so the files
// are POSIX-friendly and diff cleanly.
//
// The write is atomic (write to a temp file in the same dir, then rename) so an
// interrupted seed never leaves a half-written fixture that a later run would
// read as valid.
func writeJSONFile(path string, v any) error {
	data, err := MarshalStable(v)
	if err != nil {
		return fmt.Errorf("seed: marshal %s: %w", filepath.Base(path), err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("seed: temp file for %s: %w", filepath.Base(path), err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("seed: write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("seed: close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("seed: finalize %s: %w", filepath.Base(path), err)
	}
	return nil
}

// MarshalStable encodes v as indented JSON with a trailing newline and HTML
// escaping disabled, producing the canonical on-disk form used for every seeder
// artifact. It is exported so tests and later readers can produce / compare the
// exact same bytes. Determinism comes from json's deterministic field/slice
// ordering plus the fixed indent; the seeder never marshals a Go map into
// output, so there is no map-iteration nondeterminism.
func MarshalStable(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
