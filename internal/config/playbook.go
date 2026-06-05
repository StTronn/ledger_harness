// Package config loads the playbook — the chart of accounts plus the entry-type
// templates — from a human-readable JSON file. The playbook is the single
// source of schema truth shared by the rule engine and (later) the agent Skill.
//
// Phase 0 keeps this intentionally minimal: a typed struct and a strict loader.
// Subsequent phases flesh out Account/EntryType with template lines and the
// money semantics. Per the project invariants money is always int64 minor units;
// no float fields appear here or anywhere downstream.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Playbook is the decoded chart of accounts plus entry-type templates.
//
// The JSON shape is locked to {"accounts": [...], "entry_types": [...]} so the
// config file is forward-compatible with the richer schema later phases add.
type Playbook struct {
	Accounts   []Account   `json:"accounts"`
	EntryTypes []EntryType `json:"entry_types"`
}

// Account is a node in the chart of accounts. The Path is a Fragment-style
// segmentable path (e.g. "income/product-sales") so per-channel P&L stays
// additive later without expanding the node count.
type Account struct {
	Path string `json:"path"`
}

// EntryType is a declarative, balanced-by-construction posting template.
// Phase 0 only carries its Name; the parameterized lines are added in Phase 1.
type EntryType struct {
	Name string `json:"name"`
}

// Load reads the playbook JSON at path into a Playbook. It uses strict decoding
// so unknown fields surface as errors rather than being silently dropped — the
// playbook is a contract, and drift should fail loudly.
func Load(path string) (*Playbook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read playbook %q: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes playbook JSON from raw bytes. It is separated from Load so it is
// trivially table-testable without touching the filesystem.
func Parse(data []byte) (*Playbook, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var pb Playbook
	if err := dec.Decode(&pb); err != nil {
		return nil, fmt.Errorf("config: decode playbook: %w", err)
	}
	return &pb, nil
}
