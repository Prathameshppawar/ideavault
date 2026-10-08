package integration

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/testutil"
)

const externalTranscript = `User: We decided we will not build a marketplace for riders in version one.

Assistant: That makes sense. I assume riders will pay for premium route packs.

User: I assume riders will pay for premium route packs and group expense splitting features.

User: We need to prototype the shared route planner before the beta launch.`

func TestAnalyzeAndMergeExternalDelta(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	idea := e.MustIdea(t, u, "Delta Idea", "riders and trips")
	d1 := e.MustRecord(t, u, decision(idea.Idea.ID, "We will build a marketplace for riders in version one"))
	a1 := e.MustRecord(t, u, knowledge(idea.Idea.ID, domain.KindAssumption, "Riders will pay for premium route packs"))
	d := try(svc.AnalyzeDelta(ctx(), u.Actor(), service.AnalyzeDeltaInput{IdeaID: idea.Idea.ID, Text: externalTranscript, Provider: "chatgpt"})).must(t)
	if d.Status != "PENDING_REVIEW" || d.Analyzer != "heuristic:v1" || d.BranchID != idea.Branch.ID || d.ConversationID == nil {
		t.Fatalf("delta: %+v", d)
	}
	byClass := map[domain.DeltaClass][]domain.DeltaItem{}
	for _, it := range d.Items {
		byClass[it.Classification] = append(byClass[it.Classification], it)
	}
	check := func(cls domain.DeltaClass, kind domain.KnowledgeKind, target string) domain.DeltaItem {
		t.Helper()
		for _, it := range byClass[cls] {
			if it.Kind == kind && it.TargetLabel == target {
				return it
			}
		}
		t.Fatalf("no %s %s item targeting %q in %+v", cls, kind, target, d.Items)
		return domain.DeltaItem{}
	}
	rej := check(domain.DeltaRejected, domain.KindDecision, "D1")
	chg := check(domain.DeltaChanged, domain.KindAssumption, "A1")
	unc := check(domain.DeltaUnchanged, domain.KindAssumption, "A1")
	nw := check(domain.DeltaNew, domain.KindAction, "")
	if unc.Selected || !rej.Selected || !chg.Selected || !nw.Selected {
		t.Error("UNCHANGED items are not selected by default; changes are")
	}
	if rej.TargetStatement != d1.Statement || rej.SourceExcerpt == "" {
		t.Errorf("delta item context: %+v", rej)
	}
	conv := try(st.GetConversation(ctx(), u.ID, *d.ConversationID)).must(t)
	if conv.Origin != domain.OriginExternal || conv.Provider != "chatgpt" || conv.IdeaID == nil || *conv.IdeaID != idea.Idea.ID {
		t.Errorf("external conversation: %+v", conv)
	}
	for _, m := range try(st.ListMessages(ctx(), u.ID, conv.ID, 0, 10)).must(t) {
		if !m.Untrusted {
			t.Error("external AI messages are untrusted")
		}
	}
	src := try(st.GetSource(ctx(), u.ID, *d.SourceID)).must(t)
	if src.Kind != domain.SourceExternalAI || src.Trusted {
		t.Errorf("source: %+v", src)
	}
	// Nothing is merged until the user asks.
	if got := try(st.GetKnowledge(ctx(), u.ID, d1.ID)).must(t); got.Status != "ACTIVE" {
		t.Fatal("analysis must not change the branch")
	}
	_, err := svc.MergeDelta(ctx(), u.Actor(), d.ID, service.MergeDeltaInput{})
	wantKind(t, err, domain.KindInvalid)

	res := try(svc.MergeDelta(ctx(), u.Actor(), d.ID, service.MergeDeltaInput{ItemIDs: []uuid.UUID{rej.ID, chg.ID, nw.ID}})).must(t)
	if res.Delta.Status != "PARTIALLY_MERGED" || len(res.Merged) != 3 || res.Checkpoint == nil || res.Checkpoint.Kind != domain.CheckpointMerge {
		t.Fatalf("merge result: status=%s merged=%d cp=%+v", res.Delta.Status, len(res.Merged), res.Checkpoint)
	}
	if res.Delta.MergeCheckpointID == nil || *res.Delta.MergeCheckpointID != res.Checkpoint.ID || res.Delta.ResolvedAt == nil {
		t.Errorf("delta resolution: %+v", res.Delta)
	}
	gotD1 := try(st.GetKnowledge(ctx(), u.ID, d1.ID)).must(t)
	gotA1 := try(st.GetKnowledge(ctx(), u.ID, a1.ID)).must(t)
	if gotD1.Status != "REVERSED" || gotD1.Statement != d1.Statement {
		t.Errorf("a rejected decision is reversed by a new decision, never rewritten: %+v", gotD1)
	}
	if gotA1.Status != "SUPERSEDED" || gotA1.Statement != a1.Statement {
		t.Errorf("a changed assumption is superseded, never rewritten: %+v", gotA1)
	}
	kinds := map[domain.KnowledgeKind]int{}
	for _, m := range res.Merged {
		kinds[m.Kind]++
		if m.SourceConversationID == nil || *m.SourceConversationID != conv.ID || m.SourceID == nil || m.Origin != domain.OriginSource {
			t.Errorf("merged item provenance: %+v", m)
		}
	}
	if kinds[domain.KindDecision] != 1 || kinds[domain.KindAssumption] != 1 || kinds[domain.KindAction] != 1 {
		t.Errorf("merged kinds = %v", kinds)
	}
	merged := try(st.GetDelta(ctx(), u.ID, d.ID)).must(t)
	for _, it := range merged.Items {
		if (it.ID == unc.ID) == (it.MergedItemID != nil) {
			t.Errorf("merged_item_id bookkeeping wrong for %s: %+v", it.Classification, it)
		}
	}
	_, err = svc.MergeDelta(ctx(), u.Actor(), d.ID, service.MergeDeltaInput{ItemIDs: []uuid.UUID{unc.ID}})
	wantKind(t, err, domain.KindConflict)
	// A second delta can be discarded without touching the branch.
	d2 := try(svc.AnalyzeDelta(ctx(), u.Actor(), service.AnalyzeDeltaInput{IdeaID: idea.Idea.ID, Text: "User: We decided to skip the beta entirely because of time pressure.\n\nAssistant: Understood."})).must(t)
	before := countRows(t, `SELECT count(*) FROM knowledge_items WHERE idea_id = $1`, idea.Idea.ID)
	disc := try(svc.DiscardDelta(ctx(), u.Actor(), d2.ID)).must(t)
	if disc.Status != "DISCARDED" || countRows(t, `SELECT count(*) FROM knowledge_items WHERE idea_id = $1`, idea.Idea.ID) != before {
		t.Errorf("discard: %+v", disc)
	}
	_, err = svc.AnalyzeDelta(ctx(), u.Actor(), service.AnalyzeDeltaInput{IdeaID: idea.Idea.ID, Text: "   "})
	wantKind(t, err, domain.KindInvalid)
	list := try(st.ListDeltas(ctx(), u.ID, &idea.Idea.ID)).must(t)
	if len(list) != 2 {
		t.Errorf("deltas listed = %d", len(list))
	}
}

// seedViewsIdea creates an idea with a rich, deterministic history for the read views.
func seedViewsIdea(t *testing.T, u userT) (*service.IdeaCreated, map[string]*domain.KnowledgeItem) {
	t.Helper()
	e := env
	idea := e.MustIdea(t, u, "Biker Community Platform", "I have an idea for a biker community platform where people can create trips and invite friends.")
	id := idea.Idea.ID
	k := map[string]*domain.KnowledgeItem{}
	k["D1"] = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindDecision, Statement: "We will build a marketplace for riders",
		Rationale: "revenue", Alternatives: []domain.Alternative{{Option: "ads", ReasonRejected: "annoying"}}})
	k["A1"] = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindAssumption, Statement: "Riders will pay for premium route packs", Risk: "HIGH"})
	k["Q1"] = e.MustRecord(t, u, knowledge(id, domain.KindQuestion, "Should trips be public by default?"))
	k["E1"] = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindEvidence, Statement: "Survey shows riders plan in group chats", TargetItemID: &k["D1"].ID})
	e.MustCheckpoint(t, u, id, nil, "First direction")
	k["D2"] = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindDecision, Statement: "We will not build a marketplace for riders",
		Rationale: "moderation is expensive", Supersedes: &k["D1"].ID, Reverses: true})
	k["D3"] = e.MustRecord(t, u, decision(id, "Charge organizers a monthly fee"))
	k["D4"] = e.MustRecord(t, u, decision(id, "Ship an Android app first"))
	k["I1"] = e.MustRecord(t, u, knowledge(id, domain.KindInsight, "Organizers drive adoption"))
	k["T1"] = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindAction, Statement: "Prototype the trip planner", Priority: "HIGH"})
	e.MustCheckpoint(t, u, id, nil, "Pricing settled")
	try(e.Svc().GenerateArtifact(ctx(), u.Actor(), service.GenerateArtifactInput{IdeaID: id, Type: domain.ArtifactActionPlan}, nil)).must(t)
	try(e.Svc().ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: try(e.Svc().ResolveCheckpoint(ctx(), u.ID, id, "CP1")).must(t).ID, Name: "Marketplace direction"})).must(t)
	return idea, k
}

func TestReadViewsOnSeededIdea(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc := e.Svc()
	idea, k := seedViewsIdea(t, u)
	other := e.MustIdea(t, u, "Clinic Scheduling Assistant", "Doctors manage appointments.")
	e.DrainJobs(t)

	t.Run("today", func(t *testing.T) {
		v := try(svc.Today(ctx(), u.ID)).must(t)
		if len(v.ActiveIdeas) != 2 || v.Counts["ideas"] != 2 || v.Counts["checkpoints"] != 3 {
			t.Errorf("today ideas/counts: %d %v", len(v.ActiveIdeas), v.Counts)
		}
		if len(v.OpenQuestions) != 1 || len(v.RiskyAssumptions) != 1 || len(v.OpenActions) != 1 || v.RiskyAssumptions[0].Assumption.Risk != "HIGH" {
			t.Errorf("open loops: q=%d a=%d t=%d", len(v.OpenQuestions), len(v.RiskyAssumptions), len(v.OpenActions))
		}
		// D1 is reversed on Main but still current on the fork made from CP1, so it
		// appears once (from the fork); every other lineage appears once, from Main.
		if len(v.RecentDecisions) != 4 {
			t.Errorf("recent decisions = %d (fork copies must be de-duplicated by lineage)", len(v.RecentDecisions))
		}
		for _, d := range v.RecentDecisions {
			if d.ID == k["D1"].ID {
				t.Error("the reversed original D1 is not current")
			}
			if d.Label != "D1" && d.BranchID != idea.Branch.ID {
				t.Errorf("%s shown from a fork copy although the original on Main is current", d.Label)
			}
		}
		// Items live on both Main and the fork are shown once, from the original.
		if v.OpenQuestions[0].ID != k["Q1"].ID || v.RiskyAssumptions[0].ID != k["A1"].ID {
			t.Errorf("Today must prefer the original item over its fork copy: q=%s a=%s", v.OpenQuestions[0].BranchID, v.RiskyAssumptions[0].BranchID)
		}
		if v.IdeaTitles[idea.Idea.ID.String()] != "Biker Community Platform" || len(v.RecentChanges) == 0 {
			t.Errorf("today titles/changes: %v", v.IdeaTitles)
		}
	})
	t.Run("decisions view", func(t *testing.T) {
		v := try(svc.Decisions(ctx(), u.ID, &idea.Idea.ID)).must(t)
		if len(v.Decisions) != 4 || v.Reversals != 1 || v.Supersessions != 0 {
			t.Errorf("decisions view: %d decisions, %d reversals, %d supersessions", len(v.Decisions), v.Reversals, v.Supersessions)
		}
		for _, d := range v.Decisions {
			if d.Label == "D1" && (d.ReplacedBy == nil || d.ReplacedBy.Label != "D2" || d.EvidenceCount != 1 || d.BranchName != "Main") {
				t.Errorf("D1 entry: %+v", d)
			}
		}
	})
	t.Run("timeline", func(t *testing.T) {
		v := try(svc.Timeline(ctx(), u.ID, &idea.Idea.ID, 30)).must(t)
		if len(v.Buckets) != 1 || v.Buckets[0].Total < 10 {
			t.Errorf("timeline buckets: %+v", v.Buckets)
		}
		seen := map[string]bool{}
		for _, m := range v.Milestones {
			seen[m.EventType] = true
		}
		for _, want := range []string{"idea.created", "checkpoint.created", "decision.recorded", "decision.reversed", "artifact.created", "branch.created"} {
			if !seen[want] {
				t.Errorf("milestone %s missing: %v", want, seen)
			}
		}
	})
	t.Run("journey", func(t *testing.T) {
		j := try(svc.Journey(ctx(), u.ID, idea.Idea.ID)).must(t)
		if j[0].Kind != "origin" || j[0].Source == nil || !strings.Contains(j[0].Source.Excerpt, "biker community platform") || j[len(j)-1].Kind != "current" {
			t.Fatalf("journey ends: %+v ... %+v", j[0], j[len(j)-1])
		}
		kinds := map[string]int{}
		for i, n := range j {
			kinds[n.Kind]++
			if i > 0 && i < len(j)-1 && n.At.Before(j[i-1].At) {
				t.Error("journey must be chronological")
			}
		}
		if kinds["checkpoint"] < 2 || kinds["decision"] != 4 || kinds["artifact"] != 1 || kinds["branch"] != 1 {
			t.Errorf("journey kinds: %v", kinds)
		}
	})
	t.Run("graph", func(t *testing.T) {
		g := try(svc.BuildGraph(ctx(), u.ID, service.GraphFilter{IdeaID: &idea.Idea.ID, IncludeArtifacts: true, IncludeConversations: true})).must(t)
		nodes := map[string]service.GraphNode{}
		for _, n := range g.Nodes {
			nodes[n.ID] = n
		}
		if _, ok := nodes[k["D1"].ID.String()]; ok {
			t.Error("history is excluded unless requested")
		}
		if _, ok := nodes[k["D2"].ID.String()]; !ok {
			t.Error("live decisions are graph nodes")
		}
		branches := 0
		for _, n := range g.Nodes {
			if n.Type == domain.EntityBranch {
				branches++
			}
		}
		if branches != 2 {
			t.Errorf("idea graph shows its 2 branches, got %d", branches)
		}
		for _, edge := range g.Edges {
			if _, ok := nodes[edge.Source]; !ok {
				t.Errorf("dangling edge source %s", edge.Type)
			}
			if _, ok := nodes[edge.Target]; !ok {
				t.Errorf("dangling edge target %s", edge.Type)
			}
		}
		hist := try(svc.BuildGraph(ctx(), u.ID, service.GraphFilter{IdeaID: &idea.Idea.ID, IncludeHistory: true})).must(t)
		hasSupersedes := false
		for _, edge := range hist.Edges {
			if edge.Type == string(domain.RelSupersedes) {
				hasSupersedes = true
			}
		}
		if !hasSupersedes {
			t.Error("with history, the supersedes edge D2→D1 is drawn")
		}
		universe := try(svc.BuildGraph(ctx(), u.ID, service.GraphFilter{})).must(t)
		ideas := 0
		for _, n := range universe.Nodes {
			if n.Type == domain.EntityIdea {
				ideas++
			}
		}
		if ideas != 2 {
			t.Errorf("universe graph ideas = %d", ideas)
		}
	})
	t.Run("search", func(t *testing.T) {
		res := try(svc.Search(ctx(), u.ID, service.SearchRequest{Query: "What assumptions have never been validated?"})).must(t)
		if len(res.Results) != 2 { // A1 on Main and its copy on the fork
			t.Errorf("never-validated assumptions: %+v", res.Results)
		}
		for _, h := range res.Results {
			if h.Label != "A1" || h.Status != "UNVALIDATED" {
				t.Errorf("unexpected hit %+v", h)
			}
		}
		res = try(svc.Search(ctx(), u.ID, service.SearchRequest{Query: "Decisions related to the biker marketplace"})).must(t)
		for _, h := range res.Results {
			if h.EntityType != domain.EntityDecision {
				t.Errorf("decision search returned a %s", h.EntityType)
			}
		}
		if len(res.Results) == 0 {
			t.Error("marketplace decisions not found")
		}
		res = try(svc.Search(ctx(), u.ID, service.SearchRequest{Query: "Ideas similar to Biker Community Platform"})).must(t)
		for _, c := range res.SimilarIdeas {
			if c.Idea.ID == idea.Idea.ID {
				t.Error("an idea is not 'similar' to itself")
			}
		}
		res = try(svc.Search(ctx(), u.ID, service.SearchRequest{Query: "Where did I first talk about biker trips?"})).must(t)
		if res.Interpretation.Order != "oldest" || len(res.Results) == 0 || res.Results[0].EntityID != idea.Idea.ID {
			t.Errorf("first mention: %+v", res.Results)
		}
		_, err := svc.Search(ctx(), u.ID, service.SearchRequest{})
		wantKind(t, err, domain.KindInvalid)
		_ = other
	})
	t.Run("thinking analysis", func(t *testing.T) {
		ta := try(svc.AnalyzeThinking(ctx(), u.Actor(), nil)).must(t)
		if ta.Analyzer != "deterministic:v1" || ta.Stats["decisions"] != 4 || ta.Stats["decisions_changed"] != 1 || !strings.Contains(ta.Disclaimer, "not a psychological") {
			t.Errorf("thinking analysis: %+v", ta.Stats)
		}
		ids := map[string]bool{}
		for _, p := range ta.Patterns {
			ids[p.ID] = true
		}
		if !ids["unvalidated_assumptions"] || !ids["premature_decisions"] {
			t.Errorf("patterns: %v", ids)
		}
		scoped := try(svc.AnalyzeThinking(ctx(), u.Actor(), &other.Idea.ID)).must(t)
		if scoped.Scope != "idea" || scoped.Stats["decisions"] != 0 {
			t.Errorf("idea-scoped analysis: %+v", scoped.Stats)
		}
	})
	t.Run("prompt analysis", func(t *testing.T) {
		weak := try(svc.AnalyzePrompt(ctx(), u.Actor(), "make it better", nil)).must(t)
		strong := try(svc.AnalyzePrompt(ctx(), u.Actor(), "I'm building a Go API for my clinic app on Postgres. Write a step-by-step plan in markdown to add SMS reminders; it must cost under $20/month and include tests. For example, Twilio.", nil)).must(t)
		if weak.Overall >= strong.Overall || weak.Analyzer != "heuristic:v1" {
			t.Errorf("prompt scores weak=%d strong=%d", weak.Overall, strong.Overall)
		}
		_, err := svc.AnalyzePrompt(ctx(), u.Actor(), "   ", nil)
		wantKind(t, err, domain.KindInvalid)
		an := try(e.Store().ListAnalyses(ctx(), u.ID, "prompt", nil, 10)).must(t)
		if len(an) != 2 {
			t.Errorf("prompt analyses are stored: %d", len(an))
		}
	})
	t.Run("contradictions", func(t *testing.T) {
		fresh := e.MustIdea(t, u, "Contradiction Idea", "")
		a := e.MustRecord(t, u, decision(fresh.Idea.ID, "We will build a marketplace for riders in version one"))
		b := e.MustRecord(t, u, decision(fresh.Idea.ID, "We will not build a marketplace for riders in version one"))
		e.MustRecord(t, u, decision(fresh.Idea.ID, "Use Postgres for storage"))
		fs, analyzer := detect(t, svc, u, fresh.Idea.ID)
		if analyzer != "heuristic:v1" || len(fs) != 1 || fs[0].Kind != "contradiction" || !fs[0].Persisted {
			t.Fatalf("contradictions: %s %+v", analyzer, fs)
		}
		if !((fs[0].A.ID == a.ID && fs[0].B.ID == b.ID) || (fs[0].A.ID == b.ID && fs[0].B.ID == a.ID)) {
			t.Errorf("wrong pair: %+v", fs[0])
		}
		rels := try(e.Store().ListRelationships(ctx(), postgres.RelationshipFilter{UserID: u.ID, IdeaID: &fresh.Idea.ID, RelTypes: []domain.RelType{domain.RelContradicts}})).must(t)
		if len(rels) != 1 || rels[0].Origin != domain.OriginInterpretation {
			t.Errorf("contradiction is recorded as an interpreted relationship: %+v", rels)
		}
		// Supersessions are reported as changes in thinking on the seeded idea.
		fs2, _ := detect(t, svc, u, idea.Idea.ID)
		found := false
		for _, f := range fs2 {
			if f.Kind == "changed_decision" && f.A.Label == "D1" && f.B.Label == "D2" {
				found = true
			}
		}
		if !found {
			t.Errorf("reversal D1→D2 must be reported: %+v", fs2)
		}
	})
	t.Run("evolution", func(t *testing.T) {
		ev := try(svc.AnalyzeEvolution(ctx(), u.Actor(), idea.Idea.ID)).must(t)
		if len(ev.Steps) != 3 || ev.Steps[0].Diff != nil || ev.Steps[1].Diff == nil || len(ev.Steps[1].Diff.Superseded) != 1 || ev.Steps[2].Diff == nil {
			t.Fatalf("evolution steps: %+v", ev.Steps)
		}
		if len(ev.Versions) < 2 || len(ev.Branches) != 2 || ev.Analyzer != "deterministic:v1" {
			t.Errorf("evolution: versions=%d branches=%d", len(ev.Versions), len(ev.Branches))
		}
		if !strings.Contains(ev.Narrative, "CP1 (Main): starting state") || !strings.Contains(ev.Narrative, "CP2 (Main)") {
			t.Errorf("narrative: %q", ev.Narrative)
		}
	})
	t.Run("momentum", func(t *testing.T) {
		m := try(svc.Momentum(ctx(), u.ID, 7)).must(t)
		if len(m) == 0 || m[0].IdeaID != idea.Idea.ID || m[0].Decisions < 3 || m[0].Reversals != 1 {
			t.Errorf("momentum: %+v", m)
		}
	})
}

type userT = *testutil.User

func detect(t *testing.T, svc *service.Service, u userT, ideaID uuid.UUID) ([]service.ContradictionFinding, string) {
	t.Helper()
	fs, analyzer, err := svc.DetectContradictions(ctx(), u.Actor(), ideaID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fs, analyzer
}
