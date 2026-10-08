package chatgptv1

import (
	"strings"
	"testing"
)

// mk builds a private-use marker: mk("cite", "turn0search1") is
// U+E200 "cite" U+E202 "turn0search1" U+E201.
func mk(name string, args ...string) string {
	s := string(markStart) + name
	for _, a := range args {
		s += string(markSep) + a
	}
	return s + string(markEnd)
}

func TestCleanMarkup(t *testing.T) {
	cite1 := mk("cite", "turn0search0", "turn0search3")
	cite2 := mk("cite", "turn1search2")
	refs := []ContentReference{
		{MatchedText: cite1, Type: "grouped_webpages", Alt: "([Ride Planner](https://example.com/a))"},
		{MatchedText: cite2, Type: "grouped_webpages", Items: []RefItem{
			{Title: "Clinic [Guide]", URL: "https://example.org/guide"},
			{Title: "", URL: "https://www.example.net/x"},
			{Title: "bad", URL: "javascript:alert(1)"},
		}},
		{MatchedText: " ", Type: "sources_footnote"}, // must never replace spaces
	}
	tests := []struct {
		name, in, want string
	}{
		{"no markers", "plain text with spaces", "plain text with spaces"},
		{"alt replacement", "Plan trips. " + cite1 + "\nNext", "Plan trips. ([Ride Planner](https://example.com/a))\nNext"},
		{"items replacement", "Book slots" + cite2 + ".", "Book slots ([Clinic (Guide)](https://example.org/guide), [example.net](https://www.example.net/x))."},
		{"unmatched cite stripped with space", "End of sentence. " + mk("cite", "turn9search9") + "\nMore", "End of sentence.\nMore"},
		{"unmatched cite before punctuation", "word " + mk("cite", "turn9search9") + ", next", "word, next"},
		{"navlist stripped", "Read on." + mk("navlist", "Related", "turn0news1"), "Read on."},
		{"filecite stripped", "From your file " + mk("filecite", "turn0file0") + " we see", "From your file we see"},
		{"entity display name", "Try " + mk("entity", `["company","RoadLoop","route app"]`) + " today", "Try RoadLoop today"},
		{"entity single element", mk("entity", `["Pune"]`), "Pune"},
		{"entity broken json", "Visit " + mk("entity", `["city","Goa",`) + "!", "Visit Goa!"},
		{"entity unparseable", "Visit " + mk("entity", `nonsense`) + " soon", "Visit soon"},
		{"unterminated marker", "Tail " + string(markStart) + "cite" + string(markSep) + "turn0\nnext line", "Tail\nnext line"},
		{"stray delimiters", "a" + string(markSep) + "b" + string(markEnd), "ab"},
		{"legacy citation", "Use the template【4:0†source】.", "Use the template."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanMarkup(tt.in, refs)
			if got != tt.want {
				t.Errorf("CleanMarkup()\n got: %q\nwant: %q", got, tt.want)
			}
			if strings.ContainsAny(got, string([]rune{markStart, markEnd, markSep})) {
				t.Errorf("private-use characters left in %q", got)
			}
		})
	}
}

func TestCleanMarkupLegacyWithReference(t *testing.T) {
	refs := []ContentReference{{MatchedText: "【4:0†notes.pdf】", Alt: "([notes.pdf](https://example.com/notes.pdf))"}}
	got := CleanMarkup("See【4:0†notes.pdf】", refs)
	if got != "See([notes.pdf](https://example.com/notes.pdf))" {
		t.Fatalf("got %q", got)
	}
}
