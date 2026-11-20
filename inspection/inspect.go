// Package inspection exposes Stitch's lexical matcher for two Stripe invoice
// methods. The full service adds version proof, scoped imports and report policy.
package inspection

import "strings"

type Match struct {
	Line          int    `json:"line"`
	Excerpt       string `json:"excerpt"`
	BoundReceiver bool   `json:"boundReceiver"`
}

// InvoiceCalls locates supported Stripe call shapes in one JS or TS source file.
// BoundReceiver describes the lexical binding check only.
func InvoiceCalls(source string) []Match {
	calls := callUsages(source, []string{"invoices.retrieveUpcoming", "invoices.createPreview"})
	matches := make([]Match, 0, len(calls))
	for _, call := range calls {
		matches = append(matches, Match{
			Line:          strings.Count(source[:call.start], "\n") + 1,
			Excerpt:       source[call.start:call.end],
			BoundReceiver: call.verified,
		})
	}
	return matches
}
