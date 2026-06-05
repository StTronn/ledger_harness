package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// paths resolves the on-disk locations of a period's agent-input fixtures under
// worlds/<world>/<period>/ (SPEC §4.4). This mirrors the layout the seeder
// writes, but ingest defines it independently: ingest and seed agree only on the
// on-disk JSON contract, not on a shared Go type (the golden test guards the
// contract). root is the base directory containing the worlds/ tree (the repo
// root in normal use).
type paths struct {
	payments    string
	refunds     string
	settlements string
	disputes    string
	bankFeed    string
}

// resolvePaths builds the fixture paths for (root, world, period). It does no IO
// and no validation beyond joining — Read surfaces a missing file with a clear
// error when it actually tries to open it.
func resolvePaths(root, world, period string) paths {
	period_dir := filepath.Join(root, "worlds", world, period)
	razorpay := filepath.Join(period_dir, "razorpay")
	return paths{
		payments:    filepath.Join(razorpay, "payments.json"),
		refunds:     filepath.Join(razorpay, "refunds.json"),
		settlements: filepath.Join(razorpay, "settlements.json"),
		disputes:    filepath.Join(razorpay, "disputes.json"),
		bankFeed:    filepath.Join(period_dir, "bank-feed.json"),
	}
}

// Read loads the seeded substrate for (world, period) under root into a typed
// Raw bundle: the four Razorpay fixture files (payments/refunds/settlements/
// disputes) and the independent bank feed (SPEC §4.4). It is the fixtures-only
// ingest of Phase 3 — no live Razorpay; the raw types mirror the api shapes so
// Phase 9 can swap the source.
//
// Every expected file must exist and decode, or Read returns a clear, wrapped
// error naming the file — a missing or malformed fixture is a hard failure, not
// a silently-empty result, so a half-seeded period can never be closed as if it
// were complete. truth/gl.json is never read here (SPEC §4.4 isolation).
func Read(root, world, period string) (Raw, error) {
	p := resolvePaths(root, world, period)

	var raw Raw
	if err := readJSONArray(p.payments, &raw.Payments); err != nil {
		return Raw{}, err
	}
	if err := readJSONArray(p.refunds, &raw.Refunds); err != nil {
		return Raw{}, err
	}
	if err := readJSONArray(p.settlements, &raw.Settlements); err != nil {
		return Raw{}, err
	}
	if err := readJSONArray(p.disputes, &raw.Disputes); err != nil {
		return Raw{}, err
	}
	if err := readJSONObject(p.bankFeed, &raw.BankFeed); err != nil {
		return Raw{}, err
	}

	return raw, nil
}

// readJSONArray decodes the JSON array file at path into *out (a pointer to a
// slice). A missing file is reported with an explicit "fixture not found"
// message naming the path, so a partially-seeded period fails loudly. After a
// successful decode it guarantees the slice is non-nil (an empty "[]" file
// yields an empty, non-nil slice) so callers can range without nil checks.
func readJSONArray[T any](path string, out *[]T) error {
	data, err := readFixtureFile(path)
	if err != nil {
		return err
	}
	if err := decodeStrict(data, out); err != nil {
		return fmt.Errorf("ingest: decode %s: %w", path, err)
	}
	if *out == nil {
		*out = []T{}
	}
	return nil
}

// readJSONObject decodes the JSON object file at path into out (a pointer to a
// struct), with the same missing-file and decode-error handling as
// readJSONArray.
func readJSONObject(path string, out any) error {
	data, err := readFixtureFile(path)
	if err != nil {
		return err
	}
	if err := decodeStrict(data, out); err != nil {
		return fmt.Errorf("ingest: decode %s: %w", path, err)
	}
	return nil
}

// readFixtureFile reads path, turning a not-exist error into a clear,
// actionable message (the file is an expected fixture; its absence usually means
// the period was never seeded). Other IO errors are wrapped with the path.
func readFixtureFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("ingest: expected fixture %s not found (was the period seeded?)", path)
		}
		return nil, fmt.Errorf("ingest: read %s: %w", path, err)
	}
	return data, nil
}

// decodeStrict unmarshals data into out using a decoder that does NOT allow
// trailing garbage after the JSON value (a second top-level value is rejected).
// Unknown object keys ARE tolerated: the raw types model the subset of the
// Razorpay shape normalize needs, and a richer live api object (Phase 9) carries
// extra keys we ignore rather than reject.
func decodeStrict(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(out); err != nil {
		return err
	}
	// Reject any extra non-whitespace after the first JSON value, so a corrupt
	// fixture with appended garbage is caught rather than half-read.
	if dec.More() {
		return fmt.Errorf("unexpected trailing data after JSON value")
	}
	return nil
}
