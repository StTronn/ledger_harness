package config

import (
	_ "embed"
	"fmt"
)

// embeddedPlaybook is a byte-for-byte copy of the canonical playbook
// (config/playbook.json at the repo root) baked into the binary. It exists
// because some in-process producers — notably the deterministic seeder, which
// builds the hidden truth GL by binding the SAME entry-type templates the rest
// of the system uses — must obtain the playbook WITHOUT touching the filesystem:
// the seeder is pure with respect to (world, period) and must stay reproducible
// and free of path/cwd fragility.
//
// DRIFT GUARD: the canonical file remains config/playbook.json (SPEC §6 — the
// playbook is loaded from a config file at startup, and the learning layer edits
// THAT file, SPEC §13). This embedded copy must never diverge from it; a test
// (embed_test.go) reads both and fails the build if their bytes differ. So there
// is still a single source of truth — the embed is a verified mirror, not a
// second authority.
//
//go:embed playbook.embed.json
var embeddedPlaybook []byte

// DefaultPlaybook parses the embedded canonical playbook and returns it. It is
// the filesystem-free way to obtain the playbook in process; it runs the same
// full load-time validation as Load, so a malformed embedded copy fails loudly.
//
// It allocates a fresh *Playbook on every call (Parse builds new indexes), so a
// caller mutating the returned playbook cannot affect another caller — the
// returned value is not shared.
func DefaultPlaybook() (*Playbook, error) {
	pb, err := Parse(embeddedPlaybook)
	if err != nil {
		return nil, fmt.Errorf("config: embedded default playbook is invalid: %w", err)
	}
	return pb, nil
}
