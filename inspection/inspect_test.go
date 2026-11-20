package inspection

import (
	"encoding/json"
	"os"
	"testing"
)

func TestMatchesRecordedScan(t *testing.T) {
	raw, err := os.ReadFile("../evidence/reports.json")
	if err != nil {
		t.Fatal(err)
	}
	var export struct {
		Cases []struct {
			Key        string
			Line       int
			Caller     string
			SourceText string
		}
	}
	if err := json.Unmarshal(raw, &export); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"positive": "billing-before.ts", "compatible": "billing-after.ts"}
	for _, report := range export.Cases {
		file, ok := files[report.Key]
		if !ok {
			continue
		}
		source, err := os.ReadFile("../fixtures/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if string(source) != report.SourceText {
			t.Fatalf("%s fixture differs from recorded source", file)
		}
		got := InvoiceCalls(string(source))
		if len(got) != 1 || got[0].Line != report.Line || got[0].Excerpt != report.Caller || !got[0].BoundReceiver {
			t.Fatalf("%s scan mismatch: %+v", file, got)
		}
		delete(files, report.Key)
	}
	if len(files) != 0 {
		t.Fatalf("missing captured fixtures: %v", files)
	}
}

func TestCommentAndUnknownReceiver(t *testing.T) {
	source := "// stripe.invoices.retrieveUpcoming({})\nconst note = 'stripe.invoices.createPreview({})';\nother.invoices.retrieveUpcoming({})"
	got := InvoiceCalls(source)
	if len(got) != 1 || got[0].Line != 3 || got[0].BoundReceiver {
		t.Fatalf("unexpected lexical claim: %+v", got)
	}
}
