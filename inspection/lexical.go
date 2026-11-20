package inspection

import (
	"strings"
	"unicode"
)

// A bounded lexical scanner preserves source offsets and excludes comments/string
// contents from symbol matching. Direct Stripe bindings are verified; arbitrary
// wrappers and dynamic receivers remain candidates, never automatic proof.
type codeToken struct {
	text       string
	start, end int
	literal    bool
}

func codeTokens(s string) []codeToken {
	out := []codeToken{}
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "//" {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			i += 2
			for i+1 < len(s) && s[i:i+2] != "*/" {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		start := i
		if c == '\'' || c == '"' || c == '`' {
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i += 2
					continue
				}
				if s[i] == c {
					i++
					break
				}
				i++
			}
			if i > len(s) {
				i = len(s)
			}
			out = append(out, codeToken{s[start:i], start, i, true})
			continue
		}
		if unicode.IsLetter(rune(c)) || c == '_' || c == '$' {
			i++
			for i < len(s) && (unicode.IsLetter(rune(s[i])) || unicode.IsDigit(rune(s[i])) || s[i] == '_' || s[i] == '$') {
				i++
			}
		} else {
			i++
		}
		out = append(out, codeToken{s[start:i], start, i, false})
	}
	return out
}
func callUsages(body string, terms []string) []struct {
	start, end int
	verified   bool
} {
	ts := codeTokens(body)
	constructors := map[string]bool{}
	clients := map[string]bool{}

	for i := 0; i+3 < len(ts); i++ {
		if ts[i].text == "import" && !ts[i+1].literal && ts[i+2].text == "from" && ts[i+3].literal && strings.Trim(ts[i+3].text, "\"'") == "stripe" {
			constructors[ts[i+1].text] = true
		}
	}
	// Automatic proof is intentionally restricted to a top-level const binding
	// with no other bindings, mutations, escapes, or computed property access.
	depth := 0
	for i := 0; i < len(ts); i++ {
		if ts[i].literal {
			continue
		}
		if ts[i].text == "{" {
			depth++
		}
		if ts[i].text == "}" {
			depth--
		}
		if i+5 < len(ts) && depth == 0 && ts[i].text == "const" && ts[i+2].text == "=" && ts[i+3].text == "new" && constructors[ts[i+4].text] && ts[i+5].text == "(" {
			clients[ts[i+1].text] = true
		}
	}
	for name := range clients {
		decls := 0
		for i, t := range ts {
			if t.literal || t.text != name {
				continue
			}
			if i > 0 && ts[i-1].text == "const" {
				decls++
				continue
			}
			j := i
			for j+2 < len(ts) && ts[j+1].text == "." && !ts[j+2].literal {
				j += 2
			}
			if j == i || j+1 >= len(ts) || ts[j+1].text != "(" {
				clients[name] = false
			}

		}
		if decls != 1 || strings.Contains(body, "...") {
			clients[name] = false
		}
	}
	out := []struct {
		start, end int
		verified   bool
	}{}
	seen := map[int]bool{}
	for i, t := range ts {
		if t.literal {
			for _, term := range terms {
				if strings.HasPrefix(term, "/") && strings.Trim(t.text, "\"'`") == term && !seen[t.start] {
					out = append(out, struct {
						start, end int
						verified   bool
					}{t.start, t.end, false})
					seen[t.start] = true
				}
			}
			continue
		}
		if i > 0 && ts[i-1].text == "." {
			continue
		}
		chain := t.text
		j := i
		for j+2 < len(ts) && ts[j+1].text == "." && !ts[j+2].literal {
			chain += "." + ts[j+2].text
			j += 2
		}
		if j+1 >= len(ts) || ts[j+1].text != "(" {
			continue
		}
		for _, term := range terms {
			if chain == term || strings.HasSuffix(chain, "."+term) {
				if seen[t.start] {
					continue
				}
				end := ts[j+1].end
				depth := 1
				for k := j + 2; k < len(ts) && k < j+400; k++ {
					end = ts[k].end
					if !ts[k].literal {
						if ts[k].text == "(" {
							depth++
						}
						if ts[k].text == ")" {
							depth--
							if depth == 0 {
								break
							}
						}
					}
				}
				verified := depth == 0 && clients[t.text] && (strings.HasPrefix(chain, t.text+".invoices."))
				out = append(out, struct {
					start, end int
					verified   bool
				}{t.start, end, verified})
				seen[t.start] = true
			}
		}
	}
	return out
}
