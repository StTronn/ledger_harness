package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErr    bool
		wantAcct   int
		wantTypes  int
		firstAcct  string
		firstEntry string
	}{
		{
			name:      "empty placeholder",
			input:     `{"accounts": [], "entry_types": []}`,
			wantAcct:  0,
			wantTypes: 0,
		},
		{
			name:       "populated playbook",
			input:      `{"accounts":[{"path":"assets/bank"},{"path":"income/product-sales"}],"entry_types":[{"name":"dtc_sale"}]}`,
			wantAcct:   2,
			wantTypes:  1,
			firstAcct:  "assets/bank",
			firstEntry: "dtc_sale",
		},
		{
			name:      "missing keys default to empty slices",
			input:     `{}`,
			wantAcct:  0,
			wantTypes: 0,
		},
		{
			name:    "unknown field is rejected",
			input:   `{"accounts":[],"entry_types":[],"surprise":true}`,
			wantErr: true,
		},
		{
			name:    "malformed json is rejected",
			input:   `{not json}`,
			wantErr: true,
		},
		{
			name:    "wrong type for accounts is rejected",
			input:   `{"accounts":"oops","entry_types":[]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb, err := Parse([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}
			if got := len(pb.Accounts); got != tt.wantAcct {
				t.Errorf("len(Accounts) = %d, want %d", got, tt.wantAcct)
			}
			if got := len(pb.EntryTypes); got != tt.wantTypes {
				t.Errorf("len(EntryTypes) = %d, want %d", got, tt.wantTypes)
			}
			if tt.firstAcct != "" && pb.Accounts[0].Path != tt.firstAcct {
				t.Errorf("Accounts[0].Path = %q, want %q", pb.Accounts[0].Path, tt.firstAcct)
			}
			if tt.firstEntry != "" && pb.EntryTypes[0].Name != tt.firstEntry {
				t.Errorf("EntryTypes[0].Name = %q, want %q", pb.EntryTypes[0].Name, tt.firstEntry)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Run("reads a file from disk", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "playbook.json")
		if err := os.WriteFile(path, []byte(`{"accounts":[{"path":"assets/bank"}],"entry_types":[]}`), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		pb, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(pb.Accounts) != 1 || pb.Accounts[0].Path != "assets/bank" {
			t.Errorf("Load got %+v, want one account assets/bank", pb.Accounts)
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
			t.Fatal("Load(missing) = nil error, want error")
		}
	})

	t.Run("loads the committed placeholder playbook", func(t *testing.T) {
		// Guards that config/playbook.json stays a valid, loadable placeholder.
		path := filepath.Join("..", "..", "config", "playbook.json")
		pb, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%s): %v", path, err)
		}
		if len(pb.Accounts) != 0 || len(pb.EntryTypes) != 0 {
			t.Errorf("placeholder playbook should be empty, got %+v", pb)
		}
	})
}
