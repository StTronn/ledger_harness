package classifyq

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// store.go reads/writes the two keyed stores under runs/<world>-<period>/ (working
// state, gitignored — the async pipeline regenerates them each run). Both are
// written in the project's canonical stable JSON (2-space indent, no HTML escaping,
// trailing newline) and sorted by event_id, so a run is byte-stable and a store is
// reviewable.

const (
	proposalsFileName = "proposals.json"
	resultsFileName   = "results.json"
)

// runDir is runs/<world>-<period>/ under root — the per-run working directory the
// async stores live in (alongside trace.json / errors.json).
func runDir(root, world, period string) string {
	return filepath.Join(root, "runs", world+"-"+period)
}

// ProposalsPath / ResultsPath resolve the two store files for (world, period).
func ProposalsPath(root, world, period string) string {
	return filepath.Join(runDir(root, world, period), proposalsFileName)
}
func ResultsPath(root, world, period string) string {
	return filepath.Join(runDir(root, world, period), resultsFileName)
}

// WriteProposals sorts and writes the proposals store atomically.
func WriteProposals(path string, f ProposalsFile) error {
	sort.Slice(f.Items, func(i, j int) bool { return f.Items[i].EventID < f.Items[j].EventID })
	return writeStable(path, f, proposalsFileName)
}

// ReadProposals loads and decodes the proposals store, rejecting a wrong schema.
func ReadProposals(path string) (ProposalsFile, error) {
	var f ProposalsFile
	if err := readStrict(path, &f); err != nil {
		return ProposalsFile{}, err
	}
	if f.SchemaVersion != SchemaVersion {
		return ProposalsFile{}, fmt.Errorf("classifyq: %s schema_version=%d, want %d", path, f.SchemaVersion, SchemaVersion)
	}
	return f, nil
}

// WriteResults sorts and writes the results store atomically.
func WriteResults(path string, f ResultsFile) error {
	sort.Slice(f.Results, func(i, j int) bool { return f.Results[i].EventID < f.Results[j].EventID })
	return writeStable(path, f, resultsFileName)
}

// ReadResults loads and decodes the results store, rejecting a wrong schema.
func ReadResults(path string) (ResultsFile, error) {
	var f ResultsFile
	if err := readStrict(path, &f); err != nil {
		return ResultsFile{}, err
	}
	if f.SchemaVersion != SchemaVersion {
		return ResultsFile{}, fmt.Errorf("classifyq: %s schema_version=%d, want %d", path, f.SchemaVersion, SchemaVersion)
	}
	return f, nil
}

// readStrict reads path and JSON-decodes it into v with unknown fields rejected
// and no trailing data, surfacing a missing file with a clear message.
func readStrict(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("classifyq: %s not found (was the stage run?)", path)
		}
		return fmt.Errorf("classifyq: read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("classifyq: decode %s: %w", path, err)
	}
	if dec.More() {
		return fmt.Errorf("classifyq: %s has trailing data after the JSON value", path)
	}
	return nil
}

// writeStable marshals v to canonical JSON and writes it atomically (temp + rename),
// creating the run directory as needed.
func writeStable(path string, v any, base string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("classifyq: marshal %s: %w", base, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("classifyq: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("classifyq: temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("classifyq: write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("classifyq: close %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("classifyq: finalize %s: %w", path, err)
	}
	return nil
}
