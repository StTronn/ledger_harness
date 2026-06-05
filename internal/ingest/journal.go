package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MarshalJournal encodes a normalized event journal to canonical JSON: a
// two-space-indented array with HTML escaping disabled and a trailing newline.
// This matches the seeder/truth on-disk form (internal/seed.MarshalStable,
// internal/truth.MarshalGL) so journal artifacts across the project share one
// byte format, diff cleanly, and are byte-stable across runs.
//
// Determinism: NormalizedEvent's key order is fixed by its struct tags, the
// journal is pre-sorted by (ts, id) in Normalize, and the embedded raw objects
// are canonically re-marshalled — so there is no map-iteration nondeterminism
// anywhere in the output (SPEC §12). MarshalJournal(Normalize(raw)) is therefore
// byte-identical for identical raw, which is exactly what the golden test pins.
func MarshalJournal(events []NormalizedEvent) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(events); err != nil {
		return nil, fmt.Errorf("ingest: marshal journal: %w", err)
	}
	return buf.Bytes(), nil
}
