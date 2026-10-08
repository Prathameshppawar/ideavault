package artifacts

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/contextengine"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

func ki(kind domain.KnowledgeKind, ref int, status, stmt string) domain.KnowledgeItem {
	it := domain.KnowledgeItem{ID: uuid.New(), Kind: kind, RefNumber: ref, Label: kind.Label(ref), Status: status, Statement: stmt, ReviewState: domain.ReviewAccepted}
	it.EnsureAttrs()
	return it
}

func sampleContext() *contextengine.Context {
	d1 := ki(domain.KindDecision, 1, "ACTIVE", "Build a Chrome extension with Manifest V3 in TypeScript")
	d1.Decision.Rationale = "it can reuse the signed-in browser session"
	d1.Decision.Alternatives = []domain.Alternative{{Option: "Microsoft Graph integration", ReasonRejected: "admin consent is required"}}
	d2 := ki(domain.KindDecision, 2, "ACTIVE", "Focus the MVP on Zoho Mail attachments only")
	a1 := ki(domain.KindAssumption, 1, "UNVALIDATED", "SharePoint accepts uploads from the web UI endpoints")
	a1.Assumption.Risk = "HIGH"
	a1.Assumption.ValidationMethod = "spike against a test tenant"
	q1 := ki(domain.KindQuestion, 1, "OPEN", "How do we handle files larger than 250 MB?")
	t1 := ki(domain.KindAction, 1, "TODO", "Prototype attachment capture in Zoho Mail")
	t1.Action.Priority = "HIGH"
	old := ki(domain.KindDecision, 3, "SUPERSEDED", "Save attachments to disk first")
	br := &domain.Branch{Name: "Main"}
	return &contextengine.Context{
		Idea:   domain.Idea{Title: "Zoho to SharePoint", Summary: "Stream mail attachments into SharePoint without downloads.", Status: domain.StatusActive},
		Branch: br, Origin: "I keep downloading attachments just to upload them again.",
		Decisions: []domain.KnowledgeItem{d1, d2}, Assumptions: []domain.KnowledgeItem{a1}, Questions: []domain.KnowledgeItem{q1},
		Actions: []domain.KnowledgeItem{t1}, History: []domain.KnowledgeItem{old},
	}
}

var labelRe = regexp.MustCompile(`\[([DAEIQT]\d+)\]`)

func TestRenderDeterministicCitesRecordedThinking(t *testing.T) {
	c := sampleContext()
	md := RenderDeterministic(Get(domain.ArtifactImplementationPrompt), "Zoho → SharePoint: implementation prompt", c, "Target Chrome only")
	if !strings.HasPrefix(md, "# Zoho → SharePoint: implementation prompt\n") {
		t.Fatalf("artifact must start with an H1 title:\n%s", md)
	}
	for _, s := range Get(domain.ArtifactImplementationPrompt).Sections {
		if !strings.Contains(md, "## "+s.Title+"\n") {
			t.Errorf("missing section %q", s.Title)
		}
	}
	for _, want := range []string{
		"[D1]", "[D2]", "because it can reuse the signed-in browser session",
		"Microsoft Graph integration — rejected: admin consent is required [D1]",
		"~~Save attachments to disk first~~ [D3] (status superseded)",
		"[Q1]", "> **Instructions:** Target Chrome only", "branch Main",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
	// Every cited label refers to an item that was actually in the context (no invented labels).
	known := map[string]bool{}
	for _, it := range c.Refs() {
		known[it.Label] = true
	}
	for _, m := range labelRe.FindAllStringSubmatch(md, -1) {
		if !known[m[1]] {
			t.Errorf("artifact cites %s which is not in the recorded thinking", m[1])
		}
	}
}

func TestRenderDeterministicIsHonestAboutGaps(t *testing.T) {
	c := &contextengine.Context{Idea: domain.Idea{Title: "Empty idea"}}
	for _, at := range domain.AllArtifactTypes {
		tpl := Get(at)
		md := RenderDeterministic(tpl, "T", c, "")
		if n := strings.Count(md, "_Not yet decided — no recorded thinking about this._"); n == 0 {
			t.Errorf("%s: empty sections must say 'Not yet decided'", at)
		}
		if labelRe.MatchString(md) {
			t.Errorf("%s cites labels although nothing was recorded:\n%s", at, md)
		}
		if strings.Contains(md, "\n\n\n") {
			t.Errorf("%s has stray blank lines", at)
		}
	}
	plan := RenderDeterministic(Get(domain.ArtifactActionPlan), "Plan", sampleContext(), "")
	if !strings.Contains(plan, "- [ ] Prototype attachment capture in Zoho Mail _(high)_ [T1]") {
		t.Errorf("action plan must list recorded actions as checkboxes:\n%s", plan)
	}
	if !strings.Contains(plan, "high risk, unvalidated; validate by: spike against a test tenant") {
		t.Errorf("action plan must surface risky assumptions:\n%s", plan)
	}
	// Without recorded actions an action plan turns questions and assumptions into steps.
	c2 := sampleContext()
	c2.Actions = nil
	plan2 := RenderDeterministic(Get(domain.ArtifactActionPlan), "Plan", c2, "")
	if !strings.Contains(plan2, "- [ ] Resolve: How do we handle files larger than 250 MB? [Q1]") || !strings.Contains(plan2, "- [ ] Validate: SharePoint accepts") {
		t.Errorf("fallback steps missing:\n%s", plan2)
	}
}

func TestCitedLabels(t *testing.T) {
	md := "Use [D3] and [A1, I2]. See (CP4) and CP12. Checkbox [ ] and [x] are not citations. Not a label: D7 or [Decision]. Repeat [D3]. [T10] [Q2]. Link [Step 1](https://x)."
	items, cps := CitedLabels(md)
	if want := []string{"A1", "D3", "I2", "Q2", "T10"}; !reflect.DeepEqual(items, want) {
		t.Errorf("items = %v, want %v", items, want)
	}
	if want := []string{"CP12", "CP4"}; !reflect.DeepEqual(cps, want) {
		t.Errorf("checkpoints = %v, want %v", cps, want)
	}
	if i, c := CitedLabels("nothing here"); i != nil || c != nil {
		t.Errorf("no citations expected, got %v %v", i, c)
	}
}

func TestTemplatesRegistry(t *testing.T) {
	for _, at := range domain.AllArtifactTypes {
		tpl, ok := Templates[at]
		if !ok {
			t.Errorf("no template for %s", at)
			continue
		}
		if tpl.Type != at || tpl.Name == "" || len(tpl.Sections) == 0 || tpl.Audience == "" {
			t.Errorf("template %s incomplete: %+v", at, tpl)
		}
	}
	if Get("NOPE").Type != domain.ArtifactCustom {
		t.Error("unknown types fall back to CUSTOM")
	}
	sp := SystemPrompt(Get(domain.ArtifactActionPlan))
	for _, want := range []string{"Use ONLY the recorded thinking", "[D3]", "untrusted_imported_content", "Not yet decided"} {
		if !strings.Contains(sp, want) {
			t.Errorf("system prompt missing grounding rule %q", want)
		}
	}
	up := UserPrompt(Get(domain.ArtifactActionPlan), "My plan", sampleContext(), "be brief")
	if !strings.Contains(up, "<idea_context>") || !strings.Contains(up, "be brief") || !strings.Contains(up, "[D1]") {
		t.Errorf("user prompt incomplete:\n%s", up)
	}
}
