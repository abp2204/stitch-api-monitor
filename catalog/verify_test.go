package catalog

import (
	"strings"
	"testing"
)

func parseFixture(t *testing.T, body string) map[string]any {
	t.Helper()
	value, err := strictJSON([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	root, ok := value.(map[string]any)
	if !ok {
		t.Fatal("fixture is not an object")
	}
	return root
}
func TestStrictJSONRejectsDuplicateKeysAndTrailingInput(t *testing.T) {
	for _, body := range []string{`{"paths":{},"paths":{}}`, `{"a":{"x":1,"\u0078":2}}`, `{"a":[{"x":null,"x":3}]}`, `{} {}`, `[1,]`, "{\"x\":\"\xff\"}"} {
		if _, err := strictJSON([]byte(body)); err == nil {
			t.Fatalf("invalid JSON accepted: %q", body)
		}
	}
	value, err := strictJSON([]byte(`{"a":1e10000,"b":[true,null,"ok"]}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := canonical(value)
	if err != nil || !strings.Contains(string(data), "1e10000") {
		t.Fatal("numeric identity changed", string(data), err)
	}
}
func TestOriginalIdentityPrecedesVersionAndSyntax(t *testing.T) {
	body := `{"openapi":"3.0.0","info":{"version":"fixture"},"paths":{}}`
	identity := Original{Bytes: int64(len(body)), SHA256: sum([]byte(body)), Version: "fixture"}
	if _, err := verifyOriginal(strings.NewReader(body), identity); err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"short", "long", "changed", "version"} {
		input, pin := body, identity
		switch which {
		case "short":
			input = body[:len(body)-1]
		case "long":
			input = body + " "
		case "changed":
			input = strings.Replace(body, "fixture", "changed", 1)
		case "version":
			pin.Version = "other"
		}
		if _, err := verifyOriginal(strings.NewReader(input), pin); err == nil {
			t.Fatal("identity change accepted", which)
		}
	}
}
func TestClosureRetainsRecursiveAndEscapedReferences(t *testing.T) {
	root := parseFixture(t, `{"components":{"schemas":{"a/b~c":{"type":"object","properties":{"self":{"$ref":"#/components/schemas/a~1b~0c"},"other":{"$ref":"#/components/schemas/Other"}}},"Other":{"enum":["x","y"]}}},"operation":{"responses":{"200":{"schema":{"$ref":"#/components/schemas/a~1b~0c"}}}}}`)
	closure, err := referenceClosure(root, root["operation"])
	if err != nil {
		t.Fatal(err)
	}
	if len(closure) != 2 || closure["#/components/schemas/a~1b~0c"] == nil || closure["#/components/schemas/Other"] == nil {
		t.Fatal("incomplete recursive closure", closure)
	}
	first, err := digest(closure)
	if err != nil {
		t.Fatal(err)
	}
	other, err := referenceClosure(root, root["operation"])
	if err != nil {
		t.Fatal(err)
	}
	second, err := digest(other)
	if err != nil || first != second {
		t.Fatal("non-deterministic closure", err)
	}
	for _, ref := range []string{"https://example.test/spec.json#/a", "#/components/schemas/missing", "#/components/schemas/a~2b", "#/components/schemas/a%ZZ"} {
		if _, err := referenceClosure(root, map[string]any{"$ref": ref}); err == nil {
			t.Fatal("unresolved/unsupported reference accepted", ref)
		}
	}
	if _, err := referenceClosure(root, map[string]any{"$ref": 1}); err == nil {
		t.Fatal("non-string reference accepted")
	}
}
func TestPointerArrayAndFragmentResolution(t *testing.T) {
	root := parseFixture(t, `{"a":[{"space key":true}]}`)
	value, pointer, err := resolveLocal(root, "#/a/0/space%20key")
	if err != nil || value != true || pointer != "#/a/0/space key" {
		t.Fatal(value, pointer, err)
	}
	for _, ref := range []string{"#/a/00", "#/a/-", "#/a/1", "#/a/0/space key/child"} {
		if _, _, err := resolveLocal(root, ref); err == nil {
			t.Fatal("invalid pointer accepted", ref)
		}
	}
}
func TestCompletePathUniverseDoesNotInferAbsenceFromSelection(t *testing.T) {
	root := parseFixture(t, `{"paths":{"/unselected":{"get":{"operationId":"Keep"}},"/selected":{"post":{"operationId":"Post"}}}}`)
	all, err := fullPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Path != "/selected" || len(all[0].Methods) != 1 || all[0].Methods[0] != "POST" || all[1].Path != "/unselected" || all[1].Methods[0] != "GET" {
		t.Fatal("lost complete path/method universe", all)
	}
}
func TestPathReferencesAndMalformedMethodsFailClosed(t *testing.T) {
	root := parseFixture(t, `{"paths":{"/a":{"$ref":"#/item"}},"item":{"get":{}}}`)
	all, err := fullPaths(root)
	if err != nil || len(all) != 1 || all[0].Methods[0] != "GET" {
		t.Fatal(all, err)
	}
	for _, body := range []string{`{"paths":{"/a":{"GET":{}}}}`, `{"paths":{"/a":{"get":null}}}`, `{"paths":{"/a":{"$ref":"#/item","post":{}}},"item":{"get":{}}}`, `{"paths":{"/a":{"$ref":"#/item"}},"item":{"$ref":"#/paths/~1a"}}`, `{"paths":{"/a":{"$ref":"https://example.test/spec"}}}`} {
		if _, err := fullPaths(parseFixture(t, body)); err == nil {
			t.Fatal("ambiguous/malformed path accepted", body)
		}
	}
}
