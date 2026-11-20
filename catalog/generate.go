package catalog

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

const Schema = "stripe-invoice-v1"
const Derivation = "stripe-invoice-facts-v1"
const Changelog = "https://docs.stripe.com/changelog/basil/2025-03-31/invoice-preview-api-deprecations"
const MigrationGuide = "https://github.com/stripe/stripe-node/wiki/Migration-guide-for-v18"

type Parameter struct {
	Name            string   `json:"name"`
	In              string   `json:"in"`
	Required        bool     `json:"required"`
	Enum            []string `json:"enum,omitempty"`
	Pointer         string   `json:"pointer"`
	ResolvedPointer string   `json:"resolved_pointer"`
	SchemaPointer   string   `json:"schema_pointer,omitempty"`
	References      []string `json:"references,omitempty"`
}
type Operation struct {
	Method                  string      `json:"method"`
	Path                    string      `json:"path"`
	Present                 bool        `json:"present"`
	Pointer                 string      `json:"pointer"`
	PathItemPointer         string      `json:"path_item_pointer"`
	ResolvedPathItemPointer string      `json:"resolved_path_item_pointer,omitempty"`
	Absence                 string      `json:"absence,omitempty"`
	Origin                  string      `json:"origin,omitempty"`
	OriginPointer           string      `json:"origin_pointer,omitempty"`
	OperationID             string      `json:"operation_id,omitempty"`
	Parameters              []Parameter `json:"parameters"`
}
type Snapshot struct {
	Phase                          string      `json:"phase"`
	Original                       Original    `json:"original"`
	FullPathsCount                 int         `json:"full_paths_count"`
	FullPathsSHA256                string      `json:"full_paths_sha256"`
	FullOperationDefinitionsSHA256 string      `json:"full_operation_definitions_sha256"`
	FullReferenceClosureCount      int         `json:"full_reference_closure_count"`
	FullReferenceClosureSHA256     string      `json:"full_reference_closure_sha256"`
	DerivedFactsSHA256             string      `json:"derived_facts_sha256"`
	Operations                     []Operation `json:"operations"`
}
type Registry struct {
	Schema         string     `json:"schema"`
	Derivation     string     `json:"derivation"`
	Changelog      string     `json:"changelog"`
	MigrationGuide string     `json:"migration_guide"`
	Snapshots      []Snapshot `json:"snapshots"`
	Limitations    []string   `json:"limitations"`
}
type fullDefinitions struct {
	RootServers        any            `json:"root_servers"`
	OriginalPathItems  map[string]any `json:"original_path_items"`
	EffectivePathItems map[string]any `json:"effective_path_items"`
	Operations         map[string]any `json:"operations"`
}
type snapshotProof struct {
	Original    Original        `json:"original"`
	FullPaths   []pathMethods   `json:"full_paths"`
	Definitions fullDefinitions `json:"full_operation_definitions"`
	Closure     map[string]any  `json:"full_reference_closure"`
}
type selection struct{ Method, Path string }

var selected = []selection{{"GET", "/v1/invoices/upcoming"}, {"POST", "/v1/invoices/create_preview"}}

// Generate verifies only the two pinned originals. The proof retains complete
// definitions, effective path context, all local reference dependencies and the
// complete path/method universe OFFLINE. Only narrowly derived association facts
// are embedded. Neither output is a complete runtime OpenAPI contract.
func Generate(before, after io.Reader) ([]byte, []byte, error) {
	registry := Registry{Schema: Schema, Derivation: Derivation, Changelog: Changelog, MigrationGuide: MigrationGuide,
		Limitations: []string{"Selected invoice operations only; not a complete OpenAPI document.", "No parameter-change, request BODY or response compatibility claim.", "OpenAPI endpoint absence does not establish SDK method removal or runtime failure."}}
	proofs := []snapshotProof{}
	for _, input := range []struct {
		phase  string
		pin    Original
		reader io.Reader
	}{{"before", BeforeIdentity(), before}, {"after", AfterIdentity(), after}} {
		root, err := verifyOriginal(input.reader, input.pin)
		if err != nil {
			return nil, nil, err
		}
		snapshot, proof, err := deriveSnapshot(root, input.phase, input.pin)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", input.phase, err)
		}
		registry.Snapshots = append(registry.Snapshots, snapshot)
		proofs = append(proofs, proof)
	}
	data, err := canonical(registry)
	if err != nil {
		return nil, nil, err
	}
	proof, err := canonical(proofs)
	if err != nil {
		return nil, nil, err
	}
	return append(data, '\n'), append(proof, '\n'), nil
}

func deriveSnapshot(root map[string]any, phase string, pin Original) (Snapshot, snapshotProof, error) {
	snapshot := Snapshot{Phase: phase, Original: pin, Operations: []Operation{}}
	proof := snapshotProof{Original: pin, Definitions: fullDefinitions{RootServers: root["servers"], OriginalPathItems: map[string]any{}, EffectivePathItems: map[string]any{}, Operations: map[string]any{}}}
	universe, err := fullPaths(root)
	if err != nil {
		return snapshot, proof, err
	}
	proof.FullPaths = universe
	snapshot.FullPathsCount = len(universe)
	snapshot.FullPathsSHA256, err = digest(universe)
	if err != nil {
		return snapshot, proof, err
	}
	paths := root["paths"].(map[string]any)
	for _, target := range selected {
		itemPointer := "#/paths/" + escapePointer(target.Path)
		fact := Operation{Method: target.Method, Path: target.Path, Pointer: itemPointer + "/" + strings.ToLower(target.Method), PathItemPointer: itemPointer}
		raw, exists := paths[target.Path]
		if !exists {
			fact.Absence = "path_absent_from_full_universe"
			snapshot.Operations = append(snapshot.Operations, fact)
			continue
		}
		item, resolvedPointer, err := pathItem(root, target.Path, raw)
		if err != nil {
			return snapshot, proof, err
		}
		// Include ORIGINAL path references and the entire EFFECTIVE path item. This
		// closes inherited parameter/schema refs even when absent from method roots.
		proof.Definitions.OriginalPathItems[itemPointer] = raw
		proof.Definitions.EffectivePathItems[resolvedPointer] = item
		fact.ResolvedPathItemPointer = resolvedPointer
		value, present := item[strings.ToLower(target.Method)]
		if !present {
			fact.Absence = "method_absent_from_full_universe"
			snapshot.Operations = append(snapshot.Operations, fact)
			continue
		}
		op, ok := value.(map[string]any)
		if !ok {
			return snapshot, proof, fmt.Errorf("selected operation is not an object")
		}
		fact.Present = true
		fact.Pointer = resolvedPointer + "/" + strings.ToLower(target.Method)
		proof.Definitions.Operations[fact.Pointer] = op
		fact.OperationID, _ = op["operationId"].(string)
		if fact.OperationID == "" {
			return snapshot, proof, fmt.Errorf("selected operationId is missing")
		}
		fact.Origin, fact.OriginPointer, err = effectiveOrigin(root, item, op, resolvedPointer, fact.Pointer)
		if err != nil {
			return snapshot, proof, err
		}
		fact.Parameters, err = effectiveParameters(root, item, op, resolvedPointer, fact.Pointer)
		if err != nil {
			return snapshot, proof, err
		}
		snapshot.Operations = append(snapshot.Operations, fact)
	}
	snapshot.FullOperationDefinitionsSHA256, err = digest(proof.Definitions)
	if err != nil {
		return snapshot, proof, err
	}
	definitions, err := canonical(proof.Definitions)
	if err != nil {
		return snapshot, proof, err
	}
	// Use the same JSON object representation for closure traversal. The original
	// values retain exact JSON number tokens and complete unfiltered subtrees.
	definitionsValue, err := strictJSON(definitions)
	if err != nil {
		return snapshot, proof, err
	}
	proof.Closure, err = referenceClosure(root, definitionsValue)
	if err != nil {
		return snapshot, proof, err
	}
	snapshot.FullReferenceClosureCount = len(proof.Closure)
	snapshot.FullReferenceClosureSHA256, err = digest(proof.Closure)
	if err != nil {
		return snapshot, proof, err
	}
	snapshot.DerivedFactsSHA256, err = digest(snapshot.Operations)
	return snapshot, proof, err
}

// The existing HTTP analyzer's effective server precedence is preserved. This
// fixed registry accepts its explicit single Stripe server, not guessed origins.
func effectiveOrigin(root, item, op map[string]any, itemPointer, opPointer string) (string, string, error) {
	value, pointer := root["servers"], "#/servers"
	if servers, exists := item["servers"]; exists {
		list, ok := servers.([]any)
		if !ok {
			return "", "", fmt.Errorf("path servers must be an array")
		}
		if len(list) > 0 {
			value, pointer = servers, itemPointer+"/servers"
		}
	}
	if servers, exists := op["servers"]; exists {
		value, pointer = servers, opPointer+"/servers"
	}
	list, ok := value.([]any)
	if !ok || len(list) != 1 {
		return "", "", fmt.Errorf("selected operation requires one explicit server")
	}
	server, ok := list[0].(map[string]any)
	if !ok || server["url"] != "https://api.stripe.com/" {
		return "", "", fmt.Errorf("selected operation server is outside pinned Stripe origin")
	}
	return "https://api.stripe.com", pointer + "/0/url", nil
}

func referencedObject(root map[string]any, value any, pointer string) (map[string]any, string, []string, error) {
	refs := []string{}
	seen := map[string]bool{}
	for {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, "", nil, fmt.Errorf("reference target must be an object")
		}
		ref, exists := object["$ref"]
		if !exists {
			return object, pointer, refs, nil
		}
		text, ok := ref.(string)
		if !ok || len(object) != 1 {
			return nil, "", nil, fmt.Errorf("ambiguous reference object")
		}
		next, resolved, err := resolveLocal(root, text)
		if err != nil {
			return nil, "", nil, err
		}
		if seen[resolved] {
			return nil, "", nil, fmt.Errorf("cyclic parameter/schema reference")
		}
		seen[resolved] = true
		refs = append(refs, resolved)
		value, pointer = next, resolved
	}
}

func effectiveParameters(root, item, op map[string]any, itemPointer, opPointer string) ([]Parameter, error) {
	merged := map[string]Parameter{}
	for _, level := range []struct {
		node    map[string]any
		pointer string
	}{{item, itemPointer}, {op, opPointer}} {
		raw, exists := level.node["parameters"]
		if !exists {
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("parameters must be an array")
		}
		seen := map[string]bool{}
		for i, value := range list {
			pointer := fmt.Sprintf("%s/parameters/%d", level.pointer, i)
			object, resolved, refs, err := referencedObject(root, value, pointer)
			if err != nil {
				return nil, err
			}
			name, nameOK := object["name"].(string)
			in, inOK := object["in"].(string)
			if !nameOK || name == "" || !inOK || (in != "query" && in != "path" && in != "header" && in != "cookie") {
				return nil, fmt.Errorf("parameter name/in is invalid")
			}
			key := in + ":" + name
			if seen[key] {
				return nil, fmt.Errorf("duplicate parameter in same scope")
			}
			seen[key] = true
			p := Parameter{Name: name, In: in, Pointer: pointer, ResolvedPointer: resolved, References: refs}
			if required, exists := object["required"]; exists {
				var ok bool
				p.Required, ok = required.(bool)
				if !ok {
					return nil, fmt.Errorf("required must be boolean")
				}
			}
			if in == "path" && !p.Required {
				return nil, fmt.Errorf("path parameter must be required")
			}
			if schema, exists := object["schema"]; exists {
				definition, schemaPointer, schemaRefs, err := referencedObject(root, schema, resolved+"/schema")
				if err != nil {
					return nil, err
				}
				p.SchemaPointer = schemaPointer
				p.References = append(p.References, schemaRefs...)
				if enumeration, exists := definition["enum"]; exists {
					values, ok := enumeration.([]any)
					if !ok {
						return nil, fmt.Errorf("parameter enum must be an array")
					}
					for _, v := range values {
						if text, ok := v.(string); ok {
							p.Enum = append(p.Enum, text)
						}
					}
				}
			}
			merged[key] = p
		}
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []Parameter{}
	for _, key := range keys {
		out = append(out, merged[key])
	}
	return out, nil
}
