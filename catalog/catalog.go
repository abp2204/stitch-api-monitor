package catalog

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

//go:embed stripe-invoice-v1.json
var embedded []byte

// The embedded registry is immutable after package initialization.
var embeddedVersion = sum(embedded)

// Version identifies the DERIVED registry bytes, never either original source.
func Version() string { return embeddedVersion }

// Current returns independent facts from the reviewed embedded registry only.
// There is no runtime loader for caller metadata, files or remote originals.
func Current() (Registry, error) {
	var r Registry
	if err := json.Unmarshal(embedded, &r); err != nil {
		return r, err
	}
	if r.Schema != Schema || r.Derivation != Derivation || r.Changelog != Changelog || r.MigrationGuide != MigrationGuide || len(r.Snapshots) != 2 {
		return Registry{}, fmt.Errorf("embedded registry policy is unavailable")
	}
	for i, s := range r.Snapshots {
		phase, pin := "before", BeforeIdentity()
		if i == 1 {
			phase, pin = "after", AfterIdentity()
		}
		if s.Phase != phase || s.Original != pin || s.FullPathsCount < 1 || s.FullReferenceClosureCount < 0 || len(s.Operations) != len(selected) {
			return Registry{}, fmt.Errorf("embedded source identity is unavailable")
		}
		for _, h := range []string{s.FullPathsSHA256, s.FullOperationDefinitionsSHA256, s.FullReferenceClosureSHA256, s.DerivedFactsSHA256} {
			v, err := hex.DecodeString(h)
			if err != nil || len(v) != 32 {
				return Registry{}, fmt.Errorf("embedded derivation identity is unavailable")
			}
		}
		actual, err := digest(s.Operations)
		if err != nil || actual != s.DerivedFactsSHA256 {
			return Registry{}, fmt.Errorf("embedded derived facts changed")
		}
		for j, op := range s.Operations {
			if op.Method != selected[j].Method || op.Path != selected[j].Path || op.Present && op.Parameters == nil || !op.Present && op.Parameters != nil {
				return Registry{}, fmt.Errorf("embedded operation scope changed")
			}
		}
	}
	return r, nil
}
