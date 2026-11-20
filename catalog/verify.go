// Package catalog owns the operator-verified invoice operation registry.
// The verifier is offline: it never follows a URL or executes repository code.
package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Original struct {
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Version string `json:"version"`
}

func BeforeIdentity() Original {
	return Original{
		URL:    "https://raw.githubusercontent.com/stripe/openapi/5a411d0d1e527229cdb4d6633197ab8009899ce6/openapi/spec3.json",
		SHA256: "1199cc3c55746cb73b7be5c67b6e41f9b30b6ac428ca6c6d5ac904fa0f7b6acd", Bytes: 6823874, Version: "2025-02-24.acacia",
	}
}
func AfterIdentity() Original {
	return Original{
		URL:    "https://raw.githubusercontent.com/stripe/openapi/072fe51d85959b0ca71c1296b32725c14a78f4a8/openapi/spec3.json",
		SHA256: "5582327458972d9128a7a5f6e824d5fec72c1b934c6b5972b7e6e3e4ed1bbfaa", Bytes: 6756997, Version: "2025-03-31.basil",
	}
}

func sum(data []byte) string              { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func canonical(value any) ([]byte, error) { return json.Marshal(value) }
func digest(value any) (string, error) {
	data, err := canonical(value)
	if err != nil {
		return "", err
	}
	return sum(data), nil
}

// The extra byte detects a different original length before decoding. This is
// the immutable source identity, not a new runtime repository capacity limit.
func verifyOriginal(reader io.Reader, identity Original) (map[string]any, error) {
	data, err := io.ReadAll(io.LimitReader(reader, identity.Bytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != identity.Bytes || sum(data) != identity.SHA256 {
		return nil, fmt.Errorf("original bytes do not match pinned identity %s", identity.Version)
	}
	value, err := strictJSON(data)
	if err != nil {
		return nil, err
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("original must be a JSON object")
	}
	info, ok := root["info"].(map[string]any)
	if !ok || info["version"] != identity.Version || root["openapi"] != "3.0.0" {
		return nil, fmt.Errorf("original OpenAPI/version does not match pinned identity")
	}
	return root, nil
}

// Decode every key, including escaped spellings, before duplicate comparison.
// Numbers retain their exact JSON representation; no float conversion occurs.
func strictJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("JSON is not UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var read func() (any, error)
	read = func() (any, error) {
		token, err := dec.Token()
		if err != nil {
			return nil, err
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return token, nil
		}
		switch delim {
		case '{':
			obj := map[string]any{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("invalid object key")
				}
				if _, duplicate := obj[key]; duplicate {
					return nil, fmt.Errorf("duplicate JSON key %q", key)
				}
				value, err := read()
				if err != nil {
					return nil, err
				}
				obj[key] = value
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("unterminated JSON object")
			}
			return obj, nil
		case '[':
			values := []any{}
			for dec.More() {
				value, err := read()
				if err != nil {
					return nil, err
				}
				values = append(values, value)
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("unterminated JSON array")
			}
			return values, nil
		}
		return nil, fmt.Errorf("unexpected JSON delimiter")
	}
	value, err := read()
	if err != nil {
		return nil, err
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON input")
	}
	return value, nil
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
func resolveLocal(root any, ref string) (any, string, error) {
	if !strings.HasPrefix(ref, "#") {
		return nil, "", fmt.Errorf("external reference is unsupported: %s", ref)
	}
	pointer, err := url.PathUnescape(ref[1:])
	if err != nil || !utf8.ValidString(pointer) {
		return nil, "", fmt.Errorf("invalid reference fragment")
	}
	if pointer == "" {
		return root, "#", nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, "", fmt.Errorf("reference is not a JSON pointer")
	}
	current := root
	canonicalPointer := "#"
	for _, encoded := range strings.Split(pointer[1:], "/") {
		var key strings.Builder
		for i := 0; i < len(encoded); i++ {
			if encoded[i] != '~' {
				key.WriteByte(encoded[i])
				continue
			}
			i++
			if i >= len(encoded) || (encoded[i] != '0' && encoded[i] != '1') {
				return nil, "", fmt.Errorf("invalid pointer escape")
			}
			if encoded[i] == '0' {
				key.WriteByte('~')
			} else {
				key.WriteByte('/')
			}
		}
		segment := key.String()
		canonicalPointer += "/" + escapePointer(segment)
		switch node := current.(type) {
		case map[string]any:
			var exists bool
			current, exists = node[segment]
			if !exists {
				return nil, "", fmt.Errorf("unresolved reference %s", ref)
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) || strconv.Itoa(index) != segment {
				return nil, "", fmt.Errorf("invalid reference array index")
			}
			current = node[index]
		default:
			return nil, "", fmt.Errorf("reference traverses a scalar")
		}
	}
	return current, canonicalPointer, nil
}

// Closure retains exact complete referenced values, including schemas outside
// runtime analysis. Mark before visiting so recursive schemas terminate.
func referenceClosure(root any, selected any) (map[string]any, error) {
	closure := map[string]any{}
	var visit func(any) error
	visit = func(value any) error {
		switch node := value.(type) {
		case map[string]any:
			if value, exists := node["$ref"]; exists {
				ref, ok := value.(string)
				if !ok {
					return fmt.Errorf("$ref must be a string")
				}
				target, pointer, err := resolveLocal(root, ref)
				if err != nil {
					return err
				}
				if _, seen := closure[pointer]; !seen {
					closure[pointer] = target
					if err = visit(target); err != nil {
						return err
					}
				}
			}
			for _, child := range node {
				if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(selected); err != nil {
		return nil, err
	}
	return closure, nil
}

type pathMethods struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

var methods = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true, "head": true, "options": true, "trace": true}

func pathItem(root map[string]any, route string, raw any) (map[string]any, string, error) {
	value := raw
	pointer := "#/paths/" + escapePointer(route)
	seen := map[string]bool{}
	for {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("path item is not an object")
		}
		ref, exists := item["$ref"]
		if !exists {
			return item, pointer, nil
		}
		text, ok := ref.(string)
		if !ok || len(item) != 1 {
			return nil, "", fmt.Errorf("ambiguous path item reference")
		}
		next, canonical, err := resolveLocal(root, text)
		if err != nil {
			return nil, "", err
		}
		if seen[canonical] {
			return nil, "", fmt.Errorf("cyclic path item reference")
		}
		seen[canonical] = true
		value, pointer = next, canonical
	}
}

// This is the complete path/method universe, not just the selected operations.
// An absence fact must be checked against this complete verified enumeration.
func fullPaths(root map[string]any) ([]pathMethods, error) {
	paths, ok := root["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("OpenAPI paths must be an object")
	}
	universe := []pathMethods{}
	for route, raw := range paths {
		if !strings.HasPrefix(route, "/") {
			return nil, fmt.Errorf("invalid OpenAPI route")
		}
		item, _, err := pathItem(root, route, raw)
		if err != nil {
			return nil, err
		}
		entry := pathMethods{Path: route, Methods: []string{}}
		for key, value := range item {
			if methods[key] {
				if _, ok := value.(map[string]any); !ok {
					return nil, fmt.Errorf("operation must be an object")
				}
				entry.Methods = append(entry.Methods, strings.ToUpper(key))
			} else if key != "parameters" && key != "servers" && key != "summary" && key != "description" && !strings.HasPrefix(key, "x-") {
				return nil, fmt.Errorf("unsupported path item field %s", key)
			}
		}
		sort.Strings(entry.Methods)
		universe = append(universe, entry)
	}
	sort.Slice(universe, func(i, j int) bool { return universe[i].Path < universe[j].Path })
	return universe, nil
}
