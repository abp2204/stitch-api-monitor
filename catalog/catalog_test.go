package catalog

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

const reviewedCatalogSHA = "55587b87ea28a150e614a99c77681ce08a0d989e28ef012be43d1a2909af871e"

func TestEmbeddedInvoiceCatalogHermetic(t *testing.T) {
	if Version() != reviewedCatalogSHA {
		t.Fatal("embedded registry changed; regenerate and review exact source facts", Version())
	}
	if _, err := strictJSON(embedded); err != nil {
		t.Fatal(err)
	}
	r, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshots[0].Original != BeforeIdentity() || r.Snapshots[1].Original != AfterIdentity() {
		t.Fatal("original and derived identity conflated")
	}
	before, after := r.Snapshots[0], r.Snapshots[1]
	if before.FullPathsCount != 391 || after.FullPathsCount != 391 || before.FullReferenceClosureCount != 681 || after.FullReferenceClosureCount != 686 {
		t.Fatal("full universe/closure evidence changed")
	}
	if !before.Operations[0].Present || after.Operations[0].Present || after.Operations[0].Absence != "path_absent_from_full_universe" || !before.Operations[1].Present || !after.Operations[1].Present {
		t.Fatal("invoice endpoint presence facts changed")
	}
	if before.Operations[0].OperationID != "GetInvoicesUpcoming" || before.Operations[1].OperationID != "PostInvoicesCreatePreview" || after.Operations[1].OperationID != "PostInvoicesCreatePreview" || len(before.Operations[0].Parameters) != 26 || len(before.Operations[1].Parameters) != 0 || len(after.Operations[1].Parameters) != 0 {
		t.Fatal("request association facts changed")
	}
	for _, s := range r.Snapshots {
		for _, op := range s.Operations {
			if op.Present && (op.Origin != "https://api.stripe.com" || op.OriginPointer != "#/servers/0/url") {
				t.Fatal("effective source origin changed")
			}
		}
	}
	// Independent return values cannot mutate the production registry.
	r.Snapshots[0].Operations[0].Parameters[0].Name = "changed"
	second, err := Current()
	if err != nil || second.Snapshots[0].Operations[0].Parameters[0].Name == "changed" {
		t.Fatal("registry state aliases a consumer", err)
	}
}

func TestCatalogRegeneratePinnedOriginals(t *testing.T) {
	if os.Getenv("CATALOG_REGENERATE") != "1" {
		t.Skip("opt-in offline regeneration requires the two pinned local originals")
	}
	before, err := os.Open(os.Getenv("CATALOG_ACACIA_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	after, err := os.Open(os.Getenv("CATALOG_BASIL_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	data, rawProof, err := Generate(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, embedded) {
		t.Fatal("pinned original regeneration differs from embedded facts")
	}
	var proof []snapshotProof
	decoder := json.NewDecoder(bytes.NewReader(rawProof))
	decoder.UseNumber()
	if err = decoder.Decode(&proof); err != nil {
		t.Fatal(err)
	}
	registry, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(proof) != 2 {
		t.Fatal("missing before/after extraction proof")
	}
	for i, p := range proof {
		s := registry.Snapshots[i]
		if got, err := digest(p.FullPaths); err != nil || got != s.FullPathsSHA256 {
			t.Fatal("full path/method proof differs", i, err)
		}
		if got, err := digest(p.Definitions); err != nil || got != s.FullOperationDefinitionsSHA256 {
			t.Fatal("complete definitions/context proof differs", i, err)
		}
		if got, err := digest(p.Closure); err != nil || got != s.FullReferenceClosureSHA256 {
			t.Fatal("full closure proof differs", i, err)
		}
		// Explicitly establish context facts of THESE originals rather than assuming
		// their method objects are sufficient closure roots in every OpenAPI input.
		for _, raw := range p.Definitions.OriginalPathItems {
			item := raw.(map[string]any)
			for _, key := range []string{"$ref", "parameters", "servers"} {
				if _, exists := item[key]; exists {
					t.Fatal("pinned context changed; inspect effective derivation", i, key)
				}
			}
		}
	}
}

func TestEffectivePathContextAndParameterClosure(t *testing.T) {
	root := parseFixture(t, `{"servers":[{"url":"https://api.stripe.com/"}],"paths":{"/v1/invoices/upcoming":{"$ref":"#/paths/~1shared"},"/shared":{"parameters":[{"$ref":"#/components/parameters/Inherited"},{"name":"q","in":"query","schema":{"enum":["old"]}}],"get":{"operationId":"Invoice","parameters":[{"name":"q","in":"query","required":true,"schema":{"enum":["new"]}}],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Response"}}}}}}}},"components":{"parameters":{"Inherited":{"name":"inherited","in":"query","schema":{"$ref":"#/components/schemas/Enum"}}},"schemas":{"Enum":{"type":"string","enum":["a","b"]},"Response":{"type":"object","properties":{"next":{"$ref":"#/components/schemas/Response"}}}}}}`)
	for _, phase := range []string{"before", "after"} {
		s, proof, err := deriveSnapshot(root, phase, Original{Version: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		if s.FullOperationDefinitionsSHA256 == "" {
			t.Fatal("missing full definitions hash", phase)
		}
		fact := s.Operations[0]
		if !fact.Present || fact.ResolvedPathItemPointer != "#/paths/~1shared" || fact.Pointer != "#/paths/~1shared/get" || len(fact.Parameters) != 2 {
			t.Fatal("lost effective path context", fact)
		}
		inherited, override := fact.Parameters[0], fact.Parameters[1]
		if inherited.Name != "inherited" || inherited.ResolvedPointer != "#/components/parameters/Inherited" || inherited.SchemaPointer != "#/components/schemas/Enum" || len(inherited.References) != 2 || len(inherited.Enum) != 2 || override.Name != "q" || !override.Required || len(override.Enum) != 1 || override.Enum[0] != "new" || override.Pointer != "#/paths/~1shared/get/parameters/0" {
			t.Fatal("incorrect inheritance/override provenance", fact.Parameters)
		}
		for _, pointer := range []string{"#/paths/~1shared", "#/components/parameters/Inherited", "#/components/schemas/Enum", "#/components/schemas/Response"} {
			if _, ok := proof.Closure[pointer]; !ok {
				t.Fatal("closure omitted effective path context/ref", pointer)
			}
		}
	}
}

func TestEffectiveServerPrecedenceAndEmptyOverride(t *testing.T) {
	root := map[string]any{"servers": []any{map[string]any{"url": "https://api.stripe.com/"}}}
	item := map[string]any{"servers": []any{map[string]any{"url": "https://api.stripe.com/"}}}
	op := map[string]any{"servers": []any{map[string]any{"url": "https://api.stripe.com/"}}}
	_, pointer, err := effectiveOrigin(root, item, op, "#/item", "#/op")
	if err != nil || pointer != "#/op/servers/0/url" {
		t.Fatal(pointer, err)
	}
	delete(op, "servers")
	_, pointer, err = effectiveOrigin(root, item, op, "#/item", "#/op")
	if err != nil || pointer != "#/item/servers/0/url" {
		t.Fatal(pointer, err)
	}
	item["servers"] = []any{}
	_, pointer, err = effectiveOrigin(root, item, op, "#/item", "#/op")
	if err != nil || pointer != "#/servers/0/url" {
		t.Fatal(pointer, err)
	}
	op["servers"] = []any{}
	if _, _, err = effectiveOrigin(root, item, op, "#/item", "#/op"); err == nil {
		t.Fatal("empty operation override guessed root server")
	}
}
