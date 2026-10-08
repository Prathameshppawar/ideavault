package service

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

func TestInterpretQuery(t *testing.T) {
	tests := []struct {
		q         string
		terms     string
		types     []domain.EntityType
		statuses  []string
		order     string
		similarTo string
	}{
		{"Where did I first talk about AI automation?", "ai automation",
			[]domain.EntityType{domain.EntityMessage, domain.EntityConversation, domain.EntityIdea}, nil, "oldest", ""},
		{"Decisions related to the biker marketplace", "biker marketplace", []domain.EntityType{domain.EntityDecision}, nil, "relevance", ""},
		{"Ideas similar to HireHub", "HireHub", []domain.EntityType{domain.EntityIdea}, nil, "relevance", "HireHub"},
		{"What assumptions have never been validated?", "", []domain.EntityType{domain.EntityAssumption}, []string{"UNVALIDATED", "VALIDATING"}, "relevance", ""},
		{"open questions about pricing", "pricing", []domain.EntityType{domain.EntityQuestion}, []string{"OPEN"}, "relevance", ""},
		{"decisions we reversed", "", []domain.EntityType{domain.EntityDecision}, []string{"SUPERSEDED", "REVERSED"}, "relevance", ""},
		{"latest insights on onboarding", "onboarding", []domain.EntityType{domain.EntityInsight}, nil, "newest", ""},
		{"zoho sharepoint extension", "zoho sharepoint extension", nil, nil, "relevance", ""},
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			got := InterpretQuery(tt.q)
			if got.Terms != tt.terms {
				t.Errorf("terms = %q, want %q", got.Terms, tt.terms)
			}
			if !reflect.DeepEqual(got.Types, tt.types) {
				t.Errorf("types = %v, want %v", got.Types, tt.types)
			}
			if !reflect.DeepEqual(got.Statuses, tt.statuses) {
				t.Errorf("statuses = %v, want %v", got.Statuses, tt.statuses)
			}
			if got.Order != tt.order || got.SimilarTo != tt.similarTo {
				t.Errorf("order/similar = %q/%q, want %q/%q", got.Order, got.SimilarTo, tt.order, tt.similarTo)
			}
			if got.Explanation == "" {
				t.Error("interpretation must be explained to the user")
			}
		})
	}
}

func TestHeuristicExtract(t *testing.T) {
	msgs := []ExtractMessage{
		{Index: 0, Role: "user", Content: "I have an idea for a biker community app. We decided to skip the marketplace for v1 because moderation is expensive. Should trips be public by default?\n" +
			"I assume riders will pay for premium route packs. I need to prototype the trip planner this week."},
		{Index: 1, Role: "assistant", Content: "According to a 2025 survey, 62% of riders plan trips in group chats. The key insight is that organizers drive adoption. Let's go with private trips by default."},
		{Index: 2, Role: "system", Content: "We decided to ignore this system text completely."},
		{Index: 3, Role: "user", Content: "ok?"},
		{Index: 4, Role: "user", Content: "We decided: `func main() {}`"},
	}
	res := heuristicExtract(msgs)
	if res.Analyzer != "heuristic:v1" {
		t.Errorf("analyzer = %s", res.Analyzer)
	}
	type key struct {
		kind   domain.KnowledgeKind
		origin domain.Origin
	}
	got := map[key]string{}
	for _, it := range res.Items {
		got[key{it.Kind, it.Origin}] = it.Statement
		if it.Excerpt == "" || it.Confidence <= 0 || it.Confidence > 1 {
			t.Errorf("item %q lacks excerpt/confidence: %+v", it.Statement, it)
		}
		if it.MessageIndex == 2 || it.MessageIndex == 4 {
			t.Errorf("system messages and code must never yield knowledge: %+v", it)
		}
	}
	want := map[key]string{
		{domain.KindDecision, domain.OriginSource}:         "We decided to skip the marketplace for v1 because moderation is expensive.",
		{domain.KindQuestion, domain.OriginSource}:         "Should trips be public by default?",
		{domain.KindAssumption, domain.OriginSource}:       "I assume riders will pay for premium route packs.",
		{domain.KindAction, domain.OriginSource}:           "I need to prototype the trip planner this week.",
		{domain.KindEvidence, domain.OriginInterpretation}: "According to a 2025 survey, 62% of riders plan trips in group chats.",
		{domain.KindInsight, domain.OriginInterpretation}:  "The key insight is that organizers drive adoption.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extracted:\n%v\nwant:\n%v", got, want)
	}
	for _, it := range res.Items {
		if it.Kind == domain.KindDecision && it.MessageIndex == 1 {
			t.Error("an assistant suggestion must never become a decision")
		}
	}
	if !strings.HasPrefix(res.Title, "I have an idea for a biker community app") || res.Summary == "" {
		t.Errorf("title/summary from first user message: %q / %q", res.Title, res.Summary)
	}
	if empty := heuristicExtract(nil); len(empty.Items) != 0 {
		t.Error("no messages, no items")
	}
}

func TestCleanExtracted(t *testing.T) {
	msgs := []ExtractMessage{{Index: 0, Role: "user", Content: "We will use Postgres for everything."}}
	items := []ExtractedItem{
		{Kind: "decisions", Statement: "Use Postgres for everything", Origin: domain.OriginSource, Excerpt: "We will use Postgres", MessageIndex: 0, Confidence: 1.7},
		{Kind: "decision", Statement: "use postgres for EVERYTHING!", Origin: domain.OriginSource, Excerpt: "We will use Postgres", MessageIndex: 0, Confidence: 0.5}, // duplicate
		{Kind: "assumption", Statement: "Postgres scales", Origin: domain.OriginSource, Excerpt: "a quote that is not in the message", MessageIndex: 0, Confidence: 0.5},
		{Kind: "assumption", Statement: "Wrong index", Origin: domain.OriginSource, Excerpt: "We will use Postgres", MessageIndex: 9, Confidence: 0.5},
		{Kind: "opinion", Statement: "unknown kind dropped", Confidence: 0.5},
		{Kind: "insight", Statement: "   ", Confidence: 0.5},
		{Kind: "insight", Statement: "bad origin", Origin: "GUESS", Confidence: -2},
	}
	out := cleanExtracted(items, msgs)
	if len(out) != 4 {
		t.Fatalf("want 4 items after cleaning, got %d: %+v", len(out), out)
	}
	if out[0].Kind != domain.KindDecision || out[0].Confidence != 1 || out[0].Origin != domain.OriginSource {
		t.Errorf("first item not normalized: %+v", out[0])
	}
	if out[1].Origin != domain.OriginInterpretation || out[1].Confidence != 0.4 {
		t.Errorf("fabricated excerpt must downgrade to INTERPRETATION with lower confidence: %+v", out[1])
	}
	if out[2].Origin != domain.OriginInterpretation {
		t.Errorf("excerpt cited from a non-existent message must not be SOURCE: %+v", out[2])
	}
	if out[3].Origin != domain.OriginInterpretation || out[3].Confidence != 0 {
		t.Errorf("invalid origin/negative confidence not fixed: %+v", out[3])
	}
}

func TestHeuristicPromptAnalysis(t *testing.T) {
	weak := HeuristicPromptAnalysis("make it better")
	good := HeuristicPromptAnalysis("I'm building a Go API for my clinic app on Postgres. Write a step-by-step plan in markdown to add SMS reminders; " +
		"it must not cost more than $20/month, must use only free tiers, and should include tests. For example, Twilio or similar.")
	if weak.Overall >= 30 || good.Overall < 80 {
		t.Fatalf("scores: weak=%d good=%d", weak.Overall, good.Overall)
	}
	if len(weak.Weak) < 4 || len(weak.WhyItMatters) != len(weak.Weak) || len(weak.MissingInformation) == 0 {
		t.Errorf("weak prompt feedback incomplete: %+v", weak)
	}
	if len(good.Good) < 5 {
		t.Errorf("good prompt strengths not recognised: %+v", good.Good)
	}
	for _, d := range PromptDimensions {
		if s, ok := good.Scores[d]; !ok || s < 0 || s > 2 {
			t.Errorf("dimension %s score %d out of range", d, s)
		}
	}
	if !strings.Contains(weak.ImprovedPrompt, "[describe your situation") || !strings.Contains(weak.ImprovedPrompt, "make it better") {
		t.Errorf("improved prompt should keep intent and add placeholders: %q", weak.ImprovedPrompt)
	}
	if strings.Contains(good.ImprovedPrompt, "[describe your situation") {
		t.Error("good prompt already has context")
	}
	if weak.Analyzer != "heuristic:v1" {
		t.Errorf("analyzer = %s", weak.Analyzer)
	}
}

func item(kind domain.KnowledgeKind, ref int, lineage uuid.UUID, status, stmt string) domain.KnowledgeItem {
	return domain.KnowledgeItem{ID: uuid.New(), Kind: kind, RefNumber: ref, Label: kind.Label(ref), LineageID: lineage, Status: status, Statement: stmt,
		ReviewState: domain.ReviewAccepted}
}

func TestDiffSnapshots(t *testing.T) {
	linD1, linQ1, linA1 := uuid.New(), uuid.New(), uuid.New()
	d1 := item(domain.KindDecision, 1, linD1, "ACTIVE", "No marketplace")
	q1 := item(domain.KindQuestion, 1, linQ1, "OPEN", "Public trips?")
	a1 := item(domain.KindAssumption, 1, linA1, "UNVALIDATED", "Riders pay")
	cpA := &domain.Checkpoint{ID: uuid.New(), Label: "CP1", Title: "first", Snapshot: &domain.Snapshot{Items: []domain.KnowledgeItem{d1, q1, a1}}}

	d1b := d1
	d2 := item(domain.KindDecision, 2, uuid.New(), "ACTIVE", "Marketplace in v2")
	d1b.Status = "SUPERSEDED"
	d1b.SupersededByID = &d2.ID
	q1b := q1
	q1b.Status = "ANSWERED"
	e1 := item(domain.KindEvidence, 1, uuid.New(), "ACTIVE", "Survey says")
	cpB := &domain.Checkpoint{ID: uuid.New(), Label: "CP2", Title: "second", Snapshot: &domain.Snapshot{Items: []domain.KnowledgeItem{d1b, q1b, d2, e1}}}

	d := diffSnapshots(cpA, cpB)
	if d.From.Label != "CP1" || d.To.Label != "CP2" {
		t.Errorf("refs: %+v → %+v", d.From, d.To)
	}
	labels := func(xs []domain.KnowledgeItem) []string {
		var out []string
		for _, x := range xs {
			out = append(out, x.Label)
		}
		return out
	}
	if got := labels(d.Added); !reflect.DeepEqual(got, []string{"D2", "E1"}) {
		t.Errorf("added = %v (sorted by kind then ref)", got)
	}
	if got := labels(d.Removed); !reflect.DeepEqual(got, []string{"A1"}) {
		t.Errorf("removed = %v", got)
	}
	if len(d.Changed) != 2 {
		t.Fatalf("changed = %+v", d.Changed)
	}
	for _, c := range d.Changed {
		if !reflect.DeepEqual(c.Fields, []string{"status"}) || c.Before.LineageID != c.After.LineageID {
			t.Errorf("change %+v", c)
		}
	}
	if len(d.Superseded) != 1 || d.Superseded[0].Old.Label != "D1" || d.Superseded[0].New.Label != "D2" {
		t.Errorf("superseded = %+v", d.Superseded)
	}
	if d.Unchanged != 0 {
		t.Errorf("unchanged = %d", d.Unchanged)
	}
	same := diffSnapshots(cpA, cpA)
	if same.Unchanged != 3 || len(same.Added)+len(same.Removed)+len(same.Changed)+len(same.Superseded) != 0 {
		t.Errorf("identical checkpoints should be all unchanged: %+v", same)
	}
	if same.Added == nil || same.Removed == nil || same.Changed == nil || same.Superseded == nil {
		t.Error("diff slices must be non-nil (JSON arrays)")
	}
}

func TestSnapshotHashStableAndSensitive(t *testing.T) {
	a := item(domain.KindDecision, 1, uuid.New(), "ACTIVE", "x")
	b := item(domain.KindQuestion, 1, uuid.New(), "OPEN", "y")
	s1 := &domain.Snapshot{Idea: domain.SnapshotIdea{Title: "T", Status: domain.StatusActive}, Items: []domain.KnowledgeItem{a, b}}
	s2 := &domain.Snapshot{Idea: domain.SnapshotIdea{Title: "T", Status: domain.StatusActive}, Items: []domain.KnowledgeItem{b, a}, CapturedAt: time.Now()}
	if snapshotHash(s1) != snapshotHash(s2) {
		t.Error("hash must not depend on item order or capture time")
	}
	a2 := a
	a2.Status = "SUPERSEDED"
	s3 := &domain.Snapshot{Idea: s1.Idea, Items: []domain.KnowledgeItem{a2, b}}
	if snapshotHash(s1) == snapshotHash(s3) {
		t.Error("hash must change when an item's status changes")
	}
	s4 := &domain.Snapshot{Idea: domain.SnapshotIdea{Title: "T2", Status: domain.StatusActive}, Items: s1.Items}
	if snapshotHash(s1) == snapshotHash(s4) {
		t.Error("hash must change when the idea identity changes")
	}
	if len(snapshotHash(s1)) != 64 {
		t.Error("sha256 hex expected")
	}
}

func TestSuggestOutcome(t *testing.T) {
	mk := func(kind domain.KnowledgeKind, status string, n int) []domain.KnowledgeItem {
		var out []domain.KnowledgeItem
		for i := 0; i < n; i++ {
			out = append(out, item(kind, i+1, uuid.New(), status, "s"))
		}
		return out
	}
	join := func(xs ...[]domain.KnowledgeItem) []domain.KnowledgeItem {
		var out []domain.KnowledgeItem
		for _, x := range xs {
			out = append(out, x...)
		}
		return out
	}
	tests := []struct {
		name  string
		items []domain.KnowledgeItem
		want  domain.Outcome
	}{
		{"decisions and open actions", join(mk(domain.KindDecision, "ACTIVE", 3), mk(domain.KindAction, "TODO", 2)), domain.OutcomeActionPlan},
		{"many decisions", mk(domain.KindDecision, "ACTIVE", 4), domain.OutcomeImplement},
		{"done actions do not count", join(mk(domain.KindDecision, "ACTIVE", 3), mk(domain.KindAction, "DONE", 5)), domain.OutcomeImplement},
		{"research only", join(mk(domain.KindEvidence, "ACTIVE", 2), mk(domain.KindInsight, "ACTIVE", 1)), domain.OutcomeResearchComplete},
		{"one decision", mk(domain.KindDecision, "ACTIVE", 1), domain.OutcomeDecision},
		{"only questions", mk(domain.KindQuestion, "OPEN", 2), domain.OutcomePark},
		{"superseded decisions are history", join(mk(domain.KindDecision, "SUPERSEDED", 5), mk(domain.KindQuestion, "OPEN", 1)), domain.OutcomePark},
		{"nothing", nil, domain.OutcomeReference},
	}
	for _, tt := range tests {
		got, why := SuggestOutcome(tt.items)
		if got != tt.want || why == "" {
			t.Errorf("%s: SuggestOutcome = %s (%q), want %s", tt.name, got, why, tt.want)
		}
	}
}

func TestChunkText(t *testing.T) {
	if chunkText("   ") != nil {
		t.Error("blank text has no chunks")
	}
	if got := chunkText("short text"); len(got) != 1 || got[0] != "short text" {
		t.Errorf("short text = %v", got)
	}
	para := strings.Repeat("word ", 300) // 1500 chars
	text := strings.Join([]string{para, para, para, strings.Repeat("x", 5000)}, "\n\n")
	chunks := chunkText(text)
	if len(chunks) < 4 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	total := 0
	for i, c := range chunks {
		if len(c) > chunkChars {
			t.Errorf("chunk %d has %d chars > %d", i, len(c), chunkChars)
		}
		total += len(strings.ReplaceAll(strings.ReplaceAll(c, "\n", ""), " ", ""))
	}
	if want := len(strings.ReplaceAll(strings.ReplaceAll(text, "\n", ""), " ", "")); total != want {
		t.Errorf("chunking lost or duplicated content: %d vs %d non-space chars", total, want)
	}
	huge := chunkText(strings.Repeat(strings.Repeat("y", 1000)+"\n\n", 200))
	if len(huge) != 40 {
		t.Errorf("chunks are capped at 40, got %d", len(huge))
	}
}

func TestTokenF1(t *testing.T) {
	tests := []struct {
		ref, out string
		want     float64
	}{
		{"the cat sat", "The cat sat!", 1},
		{"a b c d", "a b x y", 0.5},
		{"skip the marketplace", "We skip the marketplace in v1", 2 * (3.0 / 6) * 1 / (3.0/6 + 1)},
		{"", "anything", 0},
		{"something", "", 0},
		{"alpha", "beta", 0},
	}
	for _, tt := range tests {
		if got := tokenF1(tt.ref, tt.out); abs(got-tt.want) > 1e-9 {
			t.Errorf("tokenF1(%q, %q) = %v, want %v", tt.ref, tt.out, got, tt.want)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestSafeFilename(t *testing.T) {
	tests := []struct{ in, want string }{
		{"conversations.json", "conversations.json"},
		{"../../etc/passwd", "passwd"},
		{`C:\Users\me\..\evil.json`, "evil.json"},
		{"/abs/path/to/export.zip", "export.zip"},
		{"..hidden", "hidden"},
		{"../..", ""},
		{"conv<script>.json", "convscript.json"},
		{"my chat (1).md", "my chat 1.md"},
		{"name\x00.json", "name.json"},
		{"résumé.txt", "rsum.txt"},
	}
	for _, tt := range tests {
		got := safeFilename(tt.in)
		if got != tt.want {
			t.Errorf("safeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if strings.ContainsAny(got, `/\`) || strings.HasPrefix(got, ".") {
			t.Errorf("safeFilename(%q) = %q is not a safe base name", tt.in, got)
		}
	}
	long := safeFilename(strings.Repeat("a", 300) + ".json")
	if len(long) != 120 || !strings.HasSuffix(long, ".json") {
		t.Errorf("long names keep their extension and are capped: %d %q", len(long), long[len(long)-10:])
	}
}

func TestHeuristicTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"I have an idea for a biker community platform where people can create trips.", "Biker Community Platform"},
		{"idea: zoho to sharepoint extension, no graph api", "Zoho to Sharepoint Extension"},
		{"Let's build a clinic scheduling assistant that texts patients", "Clinic Scheduling Assistant"},
		{"Something completely different", "fallback"},
	}
	for _, tt := range tests {
		if got := heuristicTitle(tt.in, "fallback"); got != tt.want {
			t.Errorf("heuristicTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if heuristicTitle("nothing", "") != "Untitled idea" {
		t.Error("empty fallback should become 'Untitled idea'")
	}
}

func TestHeuristicClassify(t *testing.T) {
	cur := []domain.KnowledgeItem{
		item(domain.KindDecision, 1, uuid.New(), "ACTIVE", "We will build a marketplace for riders in version one"),
		item(domain.KindAssumption, 1, uuid.New(), "UNVALIDATED", "Riders will pay for premium route packs"),
	}
	in := []ExtractedItem{
		{Kind: domain.KindDecision, Statement: "We will not build a marketplace for riders in version one"},
		{Kind: domain.KindAssumption, Statement: "Riders will pay for premium route packs"},
		{Kind: domain.KindAssumption, Statement: "Riders will pay for premium route packs and group expense splitting features"},
		{Kind: domain.KindInsight, Statement: "Organizers drive adoption of group rides"},
	}
	out := heuristicClassify(in, cur)
	want := []struct{ change, target string }{{"REJECTED", "D1"}, {"UNCHANGED", "A1"}, {"CHANGED", "A1"}, {"NEW", ""}}
	for i, w := range want {
		if out[i].Change != w.change || out[i].TargetLabel != w.target {
			t.Errorf("item %d %q: got %s/%s, want %s/%s", i, out[i].Statement, out[i].Change, out[i].TargetLabel, w.change, w.target)
		}
	}
	if jaccard("", "x") != 0 || jaccard("same words here", "same words here") != 1 {
		t.Error("jaccard edge cases")
	}
	if !negates("We won't ship it") || !negates("never again") || negates("we will ship") {
		t.Error("negation detection")
	}
}

func TestSplitSentencesAndSubstantive(t *testing.T) {
	got := splitSentences("First sentence here. Second one!\nThird line? Last")
	want := []string{"First sentence here.", "Second one!", "Third line?", "Last"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitSentences = %q", got)
	}
	if substantive("Too short.") || substantive("Heading:") || substantive("const x = { a: 1 };") || substantive("https://example.com/a/b/c/d/e/f") {
		t.Error("fragments, headings, code and bare links are not substantive")
	}
	if !substantive("We decided to skip the marketplace for now.") {
		t.Error("a normal sentence is substantive")
	}
}

func TestNormalizeTags(t *testing.T) {
	got := normalizeTags([]string{" Go ", "go", "", "AI", strings.Repeat("x", 41), "ai"})
	if !reflect.DeepEqual(got, []string{"go", "ai"}) {
		t.Errorf("normalizeTags = %v", got)
	}
	if got := normalizeTags(nil); got == nil || len(got) != 0 {
		t.Error("normalizeTags(nil) must be an empty, non-nil slice")
	}
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, strings.Repeat("t", i+1))
	}
	if len(normalizeTags(many)) != 20 {
		t.Error("tags are capped at 20")
	}
}

func TestConclusionSummaries(t *testing.T) {
	d := item(domain.KindDecision, 1, uuid.New(), "ACTIVE", "No marketplace")
	d.Decision = &domain.DecisionAttrs{Rationale: "moderation is expensive"}
	strong := item(domain.KindEvidence, 1, uuid.New(), "ACTIVE", "Two competitors failed")
	strong.Evidence = &domain.EvidenceAttrs{Strength: "STRONG"}
	weak := item(domain.KindEvidence, 2, uuid.New(), "ACTIVE", "A forum post")
	weak.Evidence = &domain.EvidenceAttrs{Strength: "WEAK"}
	learned, decisions, unresolved := conclusionSummaries([]domain.KnowledgeItem{
		d, strong, weak,
		item(domain.KindInsight, 1, uuid.New(), "ACTIVE", "Organizers drive adoption"),
		item(domain.KindQuestion, 1, uuid.New(), "OPEN", "Public trips?"),
		item(domain.KindQuestion, 2, uuid.New(), "ANSWERED", "Pricing?"),
		item(domain.KindAssumption, 1, uuid.New(), "UNVALIDATED", "Riders pay"),
	})
	if !strings.Contains(learned, "[I1] Organizers drive adoption") || !strings.Contains(learned, "[E1]") || strings.Contains(learned, "[E2]") {
		t.Errorf("learned = %q", learned)
	}
	if decisions != "- [D1] No marketplace — moderation is expensive" {
		t.Errorf("decisions = %q", decisions)
	}
	if !strings.Contains(unresolved, "[Q1]") || strings.Contains(unresolved, "[Q2]") || !strings.Contains(unresolved, "[A1] Unvalidated: Riders pay") {
		t.Errorf("unresolved = %q", unresolved)
	}
	l, dd, u := conclusionSummaries(nil)
	if l != "No insights recorded." || dd != "No decisions recorded." || u != "Nothing left unresolved." {
		t.Errorf("empty summaries: %q %q %q", l, dd, u)
	}
}

func TestDeterministicSummary(t *testing.T) {
	snap := &domain.Snapshot{Branch: domain.SnapshotBranch{Name: "Main"}}
	if got := deterministicSummary(snap); got != "No durable knowledge captured yet on Main." {
		t.Errorf("empty summary = %q", got)
	}
	snap.Items = []domain.KnowledgeItem{
		item(domain.KindDecision, 1, uuid.New(), "ACTIVE", "No marketplace"),
		item(domain.KindDecision, 2, uuid.New(), "SUPERSEDED", "Old idea"),
		item(domain.KindEvidence, 1, uuid.New(), "ACTIVE", "Survey"),
		item(domain.KindEvidence, 2, uuid.New(), "ACTIVE", "Interview"),
		item(domain.KindQuestion, 1, uuid.New(), "OPEN", "Public trips?"),
	}
	got := deterministicSummary(snap)
	for _, want := range []string{"1 decision", "2 evidence items", "1 question", "D1 No marketplace", "Open: Q1 Public trips?"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "Old idea") {
		t.Error("superseded items are not current state")
	}
}

func TestFuseReciprocalRank(t *testing.T) {
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fts := []postgres.SearchHit{{EntityID: id1, Snippet: "plain", CreatedAt: t0.Add(2 * time.Hour)}, {EntityID: id2, Snippet: "x", CreatedAt: t0}}
	sem := []postgres.SearchHit{{EntityID: id2, Snippet: "with «highlight»", CreatedAt: t0}, {EntityID: id3, CreatedAt: t0.Add(time.Hour)}}
	out := fuse(fts, sem, "relevance", 10)
	if len(out) != 3 || out[0].EntityID != id2 {
		t.Fatalf("an item found by both retrievers must rank first: %+v", out)
	}
	if out[0].Snippet != "with «highlight»" {
		t.Errorf("highlighted snippets are preferred, got %q", out[0].Snippet)
	}
	oldest := fuse(fts, sem, "oldest", 10)
	if oldest[0].EntityID != id2 || oldest[2].EntityID != id1 {
		t.Errorf("oldest order wrong: %v", oldest)
	}
	if len(fuse(fts, sem, "", 2)) != 2 {
		t.Error("limit not applied")
	}
}
