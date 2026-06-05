package truth

// io.go is the SINGLE canonical reader and writer of truth/gl.json (SPEC §4.4,
// §12). The isolation invariant is "truth/ must never be read by ingest,
// normalize, classify, reconcile, or any agent — only the scorer reads it (and
// the seeder writes it)". Concentrating the file IO here, in the package the
// guard test (isolation_test.go) polices, means the ONLY way to read or write
// the ground-truth GL file is through these functions in this package: there is
// no second JSON-unmarshal of gl.json scattered across the codebase to drift or
// to leak the file outside the allow-list.
//
//   - The seeder calls WriteTruth to emit the ground truth (it is an allowed
//     importer).
//   - The future scorer calls ReadTruth to load it (it will be added to the
//     allow-list when it lands).
//
// Anything else importing this package to call ReadTruth is exactly the
// boundary violation the guard test fails on.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MarshalGL encodes a GL to the canonical on-disk JSON form: two-space indent,
// HTML escaping disabled, and a trailing newline. This is the exact byte format
// the seeder writes, so the output is reproducible (byte-identical across runs)
// and diffs cleanly. Key order is fixed by the GL/Entry/Line struct tags and the
// entries marshal in slice order; the truth GL contains no Go maps, so there is
// no map-iteration nondeterminism. It is exported so a writer that needs the
// bytes (e.g. an atomic writer) can reuse the single canonical encoder.
func MarshalGL(gl GL) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(gl); err != nil {
		return nil, fmt.Errorf("truth: marshal GL: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteTruth writes gl to path in the canonical form (MarshalGL), atomically:
// it writes to a temp file in the same directory and renames it into place, so
// an interrupted write never leaves a half-written gl.json that ReadTruth would
// accept as valid. The destination directory must already exist (the seeder's
// layout creates it). Re-writing the same GL produces byte-identical content.
func WriteTruth(path string, gl GL) error {
	data, err := MarshalGL(gl)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("truth: temp file for %s: %w", filepath.Base(path), err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("truth: write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("truth: close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("truth: finalize %s: %w", filepath.Base(path), err)
	}
	return nil
}

// ReadTruth reads and decodes the ground-truth GL at path. It is the SINGLE
// reader of truth/gl.json in the whole system (SPEC §4.4, §12): the future
// scorer loads ground truth exclusively through here, and the isolation guard
// test forbids any non-allow-listed package from importing this package to call
// it.
//
// It validates the loaded GL is internally consistent: the schema version is
// the frozen SchemaVersion (a mismatched file is a deliberate-format-change
// error, not silently accepted), and every entry — and therefore the whole GL —
// balances (ΣDr == ΣCr). A malformed or unbalanced ground truth is a corrupt
// oracle, so it is rejected rather than scored against.
func ReadTruth(path string) (GL, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GL{}, fmt.Errorf("truth: read %s: %w", path, err)
	}

	var gl GL
	// DisallowUnknownFields keeps the frozen schema honest: an unexpected key in
	// gl.json (a drifted or hand-edited file) is surfaced, not ignored.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&gl); err != nil {
		return GL{}, fmt.Errorf("truth: decode %s: %w", path, err)
	}

	if gl.Version != SchemaVersion {
		return GL{}, fmt.Errorf("truth: %s has schema version %d, want %d (frozen)", path, gl.Version, SchemaVersion)
	}
	for _, e := range gl.Entries {
		for _, l := range e.Lines {
			if !l.Side.Valid() {
				return GL{}, fmt.Errorf("truth: %s entry %q has invalid side %q", path, e.ID, l.Side)
			}
		}
		if !e.IsBalanced() {
			dr, cr := e.SumBySide()
			return GL{}, fmt.Errorf("truth: %s entry %q does not balance (ΣDr=%s ΣCr=%s)", path, e.ID, dr, cr)
		}
	}
	if !gl.IsBalanced() {
		dr, cr := gl.SumBySide()
		return GL{}, fmt.Errorf("truth: %s does not balance (ΣDr=%s ΣCr=%s)", path, dr, cr)
	}
	return gl, nil
}
