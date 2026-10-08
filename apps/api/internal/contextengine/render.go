package contextengine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// Render produces the compact, model-facing context block. Every item carries its
// label so model output can cite it ([D3]) and IdeaVault can trace provenance.
// Imported conversation excerpts are fenced as untrusted data.
func (c *Context) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "IDEA: %s (status %s)\n", c.Idea.Title, c.Idea.Status)
	if c.Branch != nil {
		fmt.Fprintf(&b, "BRANCH: %s", c.Branch.Name)
		if c.Branch.ForkedFromCheckpointNumber != nil {
			fmt.Fprintf(&b, " (forked from CP%d)", *c.Branch.ForkedFromCheckpointNumber)
		}
		b.WriteString("\n")
	}
	if c.Checkpoint != nil {
		fmt.Fprintf(&b, "VIEWING CHECKPOINT: %s — %s (captured %s)\n", c.Checkpoint.Label, c.Checkpoint.Title, c.Checkpoint.CreatedAt.Format("2006-01-02"))
	}
	if c.Origin != "" {
		fmt.Fprintf(&b, "ORIGIN (user's own words): %s\n", trim(c.Origin, 800))
	}
	if c.Idea.Summary != "" {
		fmt.Fprintf(&b, "CURRENT UNDERSTANDING (IdeaVault interpretation): %s\n", trim(c.Idea.Summary, 800))
	}
	section := func(title string, items []domain.KnowledgeItem) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, it := range items {
			fmt.Fprintf(&b, "- [%s] %s", it.Label, it.Statement)
			if it.Status != it.Kind.DefaultStatus() {
				fmt.Fprintf(&b, " (status %s)", it.Status)
			}
			if it.Origin == domain.OriginInterpretation {
				b.WriteString(" (inferred)")
			}
			if it.Decision != nil && it.Decision.Rationale != "" {
				fmt.Fprintf(&b, " — because: %s", trim(it.Decision.Rationale, 300))
			}
			if it.Decision != nil && len(it.Decision.Alternatives) > 0 {
				var alts []string
				for _, a := range it.Decision.Alternatives {
					s := a.Option
					if a.ReasonRejected != "" {
						s += " (rejected: " + trim(a.ReasonRejected, 120) + ")"
					}
					alts = append(alts, s)
				}
				fmt.Fprintf(&b, " — alternatives: %s", strings.Join(alts, "; "))
			}
			if it.Assumption != nil && it.Assumption.Risk == "HIGH" {
				b.WriteString(" — HIGH risk")
			}
			if it.Evidence != nil && it.Evidence.Stance != "SUPPORTS" {
				fmt.Fprintf(&b, " — %s", strings.ToLower(it.Evidence.Stance))
			}
			if it.Question != nil && it.Question.Answer != "" {
				fmt.Fprintf(&b, " — answer: %s", trim(it.Question.Answer, 200))
			}
			b.WriteString("\n")
		}
	}
	section("DECISIONS", c.Decisions)
	section("ASSUMPTIONS", c.Assumptions)
	section("EVIDENCE", c.Evidence)
	section("INSIGHTS", c.Insights)
	section("OPEN QUESTIONS", c.Questions)
	section("ACTIONS", c.Actions)
	section("HOW THINKING CHANGED (superseded/reversed — historical, not current)", c.History)
	if len(c.Contradictions) > 0 {
		b.WriteString("\nCONTRADICTIONS:\n")
		for _, x := range c.Contradictions {
			fmt.Fprintf(&b, "- %s vs %s: %s\n", x.A, x.B, trim(x.Rationale, 200))
		}
	}
	if len(c.RelatedIdeas) > 0 {
		b.WriteString("\nRELATED IDEAS:\n")
		for _, r := range c.RelatedIdeas {
			fmt.Fprintf(&b, "- %s (%s)\n", r.Title, r.Why)
		}
	}
	if len(c.Checkpoints) > 0 {
		b.WriteString("\nCHECKPOINTS: ")
		var cps []string
		for _, cp := range c.Checkpoints {
			cps = append(cps, fmt.Sprintf("%s %s [%s]", cp.Label, trim(cp.Title, 50), cp.BranchName))
		}
		b.WriteString(strings.Join(lastN(cps, 12), "; ") + "\n")
	}
	if len(c.RelevantMessages) > 0 {
		b.WriteString("\nRELEVANT CONVERSATION EXCERPTS:\n")
		for _, w := range c.RelevantMessages {
			if w.Untrusted {
				fmt.Fprintf(&b, "<untrusted_imported_content conversation=%q>\n", w.Title)
			} else {
				fmt.Fprintf(&b, "<conversation_excerpt conversation=%q>\n", w.Title)
			}
			for _, m := range w.Messages {
				fmt.Fprintf(&b, "%s (#%d): %s\n", m.Role, m.Position, EscapeUntrusted(m.Content))
			}
			if w.Untrusted {
				b.WriteString("</untrusted_imported_content>\n")
			} else {
				b.WriteString("</conversation_excerpt>\n")
			}
		}
	}
	if c.Trimmed > 0 {
		fmt.Fprintf(&b, "\n(%d less relevant items omitted for brevity.)\n", c.Trimmed)
	}
	return b.String()
}

func lastN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// Refs lists every knowledge item included in the context (for provenance).
func (c *Context) Refs() []domain.KnowledgeItem {
	var out []domain.KnowledgeItem
	for _, xs := range [][]domain.KnowledgeItem{c.Decisions, c.Assumptions, c.Evidence, c.Insights, c.Questions, c.Actions, c.History} {
		out = append(out, xs...)
	}
	return out
}

// fenceTag matches anything that could open or close an untrusted-content fence.
var fenceTag = regexp.MustCompile(`(?i)<(\s*/?\s*)(untrusted_imported_content|untrusted_conversation)`)

// EscapeUntrusted neutralizes fence tags inside untrusted text, so imported or
// external content cannot close its <untrusted_imported_content> (or
// <untrusted_conversation>) wrapper and place instructions outside it.
func EscapeUntrusted(s string) string {
	return fenceTag.ReplaceAllString(s, "&lt;$1$2")
}
