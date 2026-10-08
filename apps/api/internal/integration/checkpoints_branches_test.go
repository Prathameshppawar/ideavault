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

func snapshotByLabel(cp *domain.Checkpoint) map[string]domain.KnowledgeItem {
	return labelsOf(cp.Snapshot.Items)
}

func TestCheckpointNumberingSnapshotAndHead(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	conv := &domain.Conversation{UserID: u.ID, Title: "origin chat"}
	if err := st.CreateConversation(ctx(), conv); err != nil {
		t.Fatal(err)
	}
	idea := try(svc.CreateIdea(ctx(), u.Actor(), service.CreateIdeaInput{Title: "Checkpoint Idea", OriginText: "origin words", ConversationID: &conv.ID})).must(t)
	other := e.MustIdea(t, u, "Other Idea", "")
	d1 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "No marketplace in v1", Rationale: "moderation"})
	q1 := e.MustRecord(t, u, knowledge(idea.Idea.ID, domain.KindQuestion, "Public trips by default?"))
	e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindInsight, Statement: "proposal only", Proposed: true})

	cp1 := try(svc.CreateCheckpoint(ctx(), u.Actor(), service.CreateCheckpointInput{IdeaID: idea.Idea.ID, Context: "Before pricing talks"})).must(t)
	if cp1.Number != 1 || cp1.Label != "CP1" || cp1.Kind != domain.CheckpointManual || cp1.BranchID != idea.Branch.ID || cp1.ParentCheckpointID != nil {
		t.Errorf("CP1 = %+v", cp1)
	}
	if cp1.Title != "After D1: No marketplace in v1" || !strings.Contains(cp1.Summary, "1 decision") || !strings.Contains(cp1.Summary, "Open: Q1") {
		t.Errorf("default title/summary: %q / %q", cp1.Title, cp1.Summary)
	}
	if len(cp1.ContentHash) != 64 || cp1.Counts["decision"] != 1 || cp1.Counts["question"] != 1 || cp1.Counts["insight"] != 0 {
		t.Errorf("hash/counts: %q %v (proposals are not snapshotted)", cp1.ContentHash, cp1.Counts)
	}
	snap := cp1.Snapshot
	if snap.Idea.Title != "Checkpoint Idea" || snap.Idea.OriginText != "origin words" || snap.Branch.Name != "Main" || snap.Context != "Before pricing talks" {
		t.Errorf("snapshot identity: %+v %+v", snap.Idea, snap.Branch)
	}
	if len(snap.Conversations) != 1 || snap.Conversations[0].ID != conv.ID {
		t.Errorf("snapshot conversations: %+v", snap.Conversations)
	}
	br := try(st.GetBranch(ctx(), u.ID, idea.Branch.ID)).must(t)
	if br.HeadCheckpointID == nil || *br.HeadCheckpointID != cp1.ID || br.CheckpointCount != 1 {
		t.Errorf("branch head must point at CP1: %+v", br)
	}
	// Numbering is per idea.
	ocp := e.MustCheckpoint(t, u, other.Idea.ID, nil, "")
	if ocp.Number != 1 {
		t.Errorf("another idea starts at CP1, got CP%d", ocp.Number)
	}
	if ocp.Title != "Snapshot of 0 knowledge items" || !strings.HasPrefix(ocp.Summary, "No durable knowledge") {
		t.Errorf("empty checkpoint: %q %q", ocp.Title, ocp.Summary)
	}

	// Change thinking after CP1: supersede D1, answer Q1, add E1.
	d2 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "Marketplace for verified riders", Supersedes: &d1.ID})
	try(svc.UpdateKnowledgeStatus(ctx(), u.Actor(), q1.ID, service.UpdateKnowledgeStatusInput{Status: "ANSWERED", Answer: "Private"})).must(t)
	e.MustRecord(t, u, knowledge(idea.Idea.ID, domain.KindEvidence, "Survey of 40 riders"))
	cp2 := e.MustCheckpoint(t, u, idea.Idea.ID, nil, "After pricing")
	if cp2.Number != 2 || cp2.ParentCheckpointID == nil || *cp2.ParentCheckpointID != cp1.ID || cp2.ContentHash == cp1.ContentHash {
		t.Errorf("CP2 = %+v", cp2)
	}

	// CP1 is immutable: re-reading it returns exactly the state at CP1.
	again := try(st.GetCheckpoint(ctx(), u.ID, cp1.ID)).must(t)
	items := snapshotByLabel(again)
	if len(items) != 2 || items["D1"].Status != "ACTIVE" || items["D1"].SupersededByID != nil || items["Q1"].Status != "OPEN" {
		t.Errorf("CP1 snapshot changed after later edits: %+v", items)
	}
	if _, ok := items["D2"]; ok {
		t.Error("CP1 must not contain later decisions")
	}
	if again.ContentHash != cp1.ContentHash || again.Title != cp1.Title || again.Summary != cp1.Summary {
		t.Error("CP1 metadata changed")
	}
	if again.Snapshot.Items[0].Decision == nil && again.Snapshot.Items[1].Decision == nil {
		t.Error("snapshot items must embed type-specific attributes")
	}
	byLabel := snapshotByLabel(cp2)
	if byLabel["D1"].Status != "SUPERSEDED" || byLabel["D2"].Status != "ACTIVE" || byLabel["Q1"].Status != "ANSWERED" || len(byLabel) != 4 {
		t.Errorf("CP2 snapshot = %+v", byLabel)
	}
	ci := countRows(t, `SELECT count(*) FROM checkpoint_items WHERE checkpoint_id = $1`, cp2.ID)
	if ci != 4 {
		t.Errorf("checkpoint_items index rows = %d", ci)
	}
	containing := try(st.CheckpointsContaining(ctx(), u.ID, d1.ID)).must(t)
	if len(containing) != 2 || containing[0].Label != "CP1" {
		t.Errorf("D1 is in CP1 and CP2: %+v", containing)
	}
	// Resolution by label/number/uuid.
	for _, ref := range []string{"CP2", "cp 2", "2", "checkpoint 2", "#2", cp2.ID.String()} {
		got, err := svc.ResolveCheckpoint(ctx(), u.ID, idea.Idea.ID, ref)
		if err != nil || got.ID != cp2.ID {
			t.Errorf("ResolveCheckpoint(%q) = %v, %v", ref, got, err)
		}
	}
	_, err := svc.ResolveCheckpoint(ctx(), u.ID, idea.Idea.ID, "latest")
	wantKind(t, err, domain.KindInvalid)
	_, err = svc.ResolveCheckpoint(ctx(), u.ID, idea.Idea.ID, "CP9")
	wantKind(t, err, domain.KindNotFound)
	_, err = svc.ResolveCheckpoint(ctx(), u.ID, other.Idea.ID, cp2.ID.String())
	wantKind(t, err, domain.KindInvalid)

	// Compare CP1 → CP2.
	diff := try(svc.CompareCheckpoints(ctx(), u.ID, cp1.ID, cp2.ID)).must(t)
	if len(diff.Added) != 2 || len(diff.Changed) != 2 || len(diff.Superseded) != 1 || diff.Superseded[0].Old.Label != "D1" || diff.Superseded[0].New.Label != "D2" {
		t.Errorf("diff = added %d changed %d superseded %+v", len(diff.Added), len(diff.Changed), diff.Superseded)
	}
	_ = d2
	_, err = svc.CompareCheckpoints(ctx(), u.ID, cp1.ID, ocp.ID)
	wantKind(t, err, domain.KindInvalid)
}

// seedTimeline builds Main with CP1..CP7 on an idea and returns the checkpoints by number.
// D1/A1/Q1 exist from CP1; D1 is superseded by D2 after CP4; E1 and I1 are added after CP4.
type timeline struct {
	idea            *service.IdeaCreated
	cps             map[int]*domain.Checkpoint
	d1, d2, a1, q1  *domain.KnowledgeItem
	e1, i1, t1, d3  *domain.KnowledgeItem
	evidenceOfD1    *domain.KnowledgeItem
	supportsOrigRel uuid.UUID
}

func seedTimeline(t *testing.T, e *testutil.Env, u *testutil.User, title string) *timeline {
	t.Helper()
	tl := &timeline{cps: map[int]*domain.Checkpoint{}}
	tl.idea = e.MustIdea(t, u, title, "A platform where riders create trips and invite friends.")
	id := tl.idea.Idea.ID
	tl.d1 = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindDecision, Statement: "We will not build a marketplace in v1",
		Rationale: "moderation is expensive", Alternatives: []domain.Alternative{{Option: "Escrow marketplace", ReasonRejected: "compliance"}}})
	tl.a1 = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindAssumption, Statement: "Riders will pay for route packs", Risk: "HIGH"})
	tl.q1 = e.MustRecord(t, u, knowledge(id, domain.KindQuestion, "Should trips be public by default?"))
	tl.evidenceOfD1 = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindEvidence, Statement: "Two marketplaces shut down citing fraud", TargetItemID: &tl.d1.ID})
	for n := 1; n <= 4; n++ {
		tl.cps[n] = e.MustCheckpoint(t, u, id, nil, "")
	}
	tl.d2 = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindDecision, Statement: "Build a marketplace for verified riders", Supersedes: &tl.d1.ID})
	tl.cps[5] = e.MustCheckpoint(t, u, id, nil, "")
	tl.e1 = e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindEvidence, Statement: "Survey: 62% of riders plan trips in group chats", Strength: "STRONG"})
	tl.i1 = e.MustRecord(t, u, knowledge(id, domain.KindInsight, "Organizers drive adoption"))
	tl.t1 = e.MustRecord(t, u, knowledge(id, domain.KindAction, "Prototype the trip planner"))
	tl.cps[6] = e.MustCheckpoint(t, u, id, nil, "")
	tl.d3 = e.MustRecord(t, u, decision(id, "Charge organizers, not riders"))
	tl.cps[7] = e.MustCheckpoint(t, u, id, nil, "Research done")
	for n := 1; n <= 7; n++ {
		if tl.cps[n].Number != n {
			t.Fatalf("checkpoint %d has number %d", n, tl.cps[n].Number)
		}
	}
	return tl
}

func TestForkFromCheckpointRestoresStateAsOfCheckpoint(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	tl := seedTimeline(t, e, u, "Fork Idea")
	mainBefore := try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, BranchID: &tl.idea.Branch.ID, Limit: 1000})).must(t)

	res := try(svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: tl.cps[4].ID, Name: "Marketplace-free direction"})).must(t)
	fork := res.Branch
	if fork.ParentBranchID == nil || *fork.ParentBranchID != tl.idea.Branch.ID || fork.ForkedFromCheckpointID == nil || *fork.ForkedFromCheckpointID != tl.cps[4].ID ||
		fork.ForkMode == nil || *fork.ForkMode != domain.ForkCheckpoint || fork.ForkedFromCheckpointNumber == nil || *fork.ForkedFromCheckpointNumber != 4 || fork.IsDefault {
		t.Errorf("fork metadata: %+v", fork)
	}
	if res.FromCheckpointN != 4 || res.SelectedN != 0 {
		t.Errorf("inherited %d, selected %d", res.FromCheckpointN, res.SelectedN)
	}
	items := try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, BranchID: &fork.ID, Limit: 1000})).must(t)
	got := labelsOf(items)
	if len(got) != 4 {
		t.Fatalf("fork must contain exactly CP4's 4 items, got %v", keysOf(got))
	}
	d1 := got["D1"]
	if d1.Status != "ACTIVE" || d1.SupersededByID != nil {
		t.Errorf("D1 was superseded AFTER CP4, so it must be ACTIVE in the fork: %+v", d1)
	}
	if d1.InheritedFromID == nil || *d1.InheritedFromID != tl.d1.ID || d1.LineageID != tl.d1.LineageID || d1.ID == tl.d1.ID {
		t.Errorf("fork copies keep lineage and provenance: %+v", d1)
	}
	if d1.Decision.Rationale != "moderation is expensive" || len(d1.Decision.Alternatives) != 1 {
		t.Errorf("type-specific attributes are copied: %+v", d1.Decision)
	}
	for _, later := range []string{"D2", "D3", "E2", "I1", "T1"} {
		if _, ok := got[later]; ok {
			t.Errorf("%s was recorded after CP4 and must not be in the fork", later)
		}
	}
	// The evidence → decision link is recreated between the copies.
	rels := try(st.ListRelationships(ctx(), postgres.RelationshipFilter{UserID: u.ID, EntityID: &d1.ID, RelTypes: []domain.RelType{domain.RelSupports}})).must(t)
	if len(rels) != 1 || rels[0].FromID != got["E1"].ID {
		t.Errorf("supports edge must connect the copied evidence and decision: %+v", rels)
	}
	inh := try(st.ListInheritance(ctx(), u.ID, fork.ID)).must(t)
	nItems := 0
	for _, r := range inh {
		if r.EntityType.IsKnowledge() {
			nItems++
			if r.Via != "checkpoint" || r.SourceCheckpointID == nil || *r.SourceCheckpointID != tl.cps[4].ID || r.NewEntityID == nil || r.Label == "" {
				t.Errorf("inheritance record: %+v", r)
			}
		}
	}
	if nItems != 4 {
		t.Errorf("inheritance records for items = %d", nItems)
	}
	// Fork base checkpoint records the starting state immutably.
	base := res.BaseCheckpoint
	if base == nil || base.Kind != domain.CheckpointForkBase || base.BranchID != fork.ID || base.Number != 8 || len(base.Snapshot.Items) != 4 {
		t.Fatalf("fork base checkpoint: %+v", base)
	}
	head := try(st.GetBranch(ctx(), u.ID, fork.ID)).must(t)
	if head.HeadCheckpointID == nil || *head.HeadCheckpointID != base.ID {
		t.Error("fork head must be its base checkpoint")
	}
	// The original branch is unchanged.
	mainAfter := try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, BranchID: &tl.idea.Branch.ID, Limit: 1000})).must(t)
	if len(mainAfter) != len(mainBefore) {
		t.Errorf("forking changed Main: %d → %d items", len(mainBefore), len(mainAfter))
	}
	if m := labelsOf(mainAfter); m["D1"].Status != "SUPERSEDED" {
		t.Error("Main's D1 must still be superseded")
	}
	// New thinking on the fork does not leak into Main and continues idea-wide numbering.
	d := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: tl.idea.Idea.ID, BranchID: &fork.ID, Kind: domain.KindDecision, Statement: "Sponsored trips instead"})
	if d.Label != "D4" || d.BranchID != fork.ID {
		t.Errorf("fork decision label %s", d.Label)
	}
	if len(try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, BranchID: &tl.idea.Branch.ID, Limit: 1000})).must(t)) != len(mainBefore) {
		t.Error("fork writes leaked into Main")
	}
	// Superseding inside the fork works on the copy only.
	fd1 := got["D1"]
	e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: tl.idea.Idea.ID, BranchID: &fork.ID, Kind: domain.KindDecision, Statement: "Marketplace later", Supersedes: &fd1.ID})
	if orig := try(st.GetKnowledge(ctx(), u.ID, tl.d1.ID)).must(t); *orig.SupersededByID != tl.d2.ID {
		t.Error("superseding the fork copy must not touch Main's D1")
	}
	// Overview of the fork shows inheritance.
	ov := try(svc.IdeaOverview(ctx(), u.ID, tl.idea.Idea.ID, &fork.ID)).must(t)
	if len(ov.Inheritance) == 0 || len(ov.Branches) != 2 || ov.Branch.ID != fork.ID {
		t.Errorf("overview of fork: inheritance=%d branches=%d", len(ov.Inheritance), len(ov.Branches))
	}
}

func keysOf(m map[string]domain.KnowledgeItem) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSelectiveForkBringsOnlySelectedLaterResearch(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	tl := seedTimeline(t, e, u, "Selective Fork Idea")
	res := try(svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: tl.cps[4].ID, Name: "CP4 + research",
		Selections: []service.Selection{{CheckpointID: &tl.cps[7].ID, Kinds: []domain.KnowledgeKind{domain.KindEvidence, "insights"}}}})).must(t)
	if *res.Branch.ForkMode != domain.ForkSelective || res.FromCheckpointN != 4 {
		t.Errorf("fork mode/inherited: %v %d", *res.Branch.ForkMode, res.FromCheckpointN)
	}
	if res.SelectedN != 2 {
		t.Errorf("exactly the new evidence and insight from CP7 should be brought, got %d", res.SelectedN)
	}
	got := labelsOf(try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, BranchID: &res.Branch.ID, Limit: 1000})).must(t))
	for _, want := range []string{"D1", "A1", "Q1", "E1", "E2", "I1"} {
		if _, ok := got[want]; !ok {
			t.Errorf("selective fork is missing %s (has %v)", want, keysOf(got))
		}
	}
	for _, not := range []string{"D2", "D3", "T1"} {
		if _, ok := got[not]; ok {
			t.Errorf("%s was not selected and must not be brought", not)
		}
	}
	if got["D1"].Status != "ACTIVE" {
		t.Error("CP4 state of D1 must be kept even though CP7 has it superseded")
	}
	sel := 0
	for _, r := range try(st.ListInheritance(ctx(), u.ID, res.Branch.ID)).must(t) {
		if r.Via == "selection" {
			sel++
			if r.SourceCheckpointID == nil || *r.SourceCheckpointID != tl.cps[7].ID {
				t.Errorf("selection provenance must point at CP7: %+v", r)
			}
		}
	}
	if sel != 2 {
		t.Errorf("selection inheritance records = %d", sel)
	}
	// Selecting from another idea is rejected; specific items by id work.
	other := seedTimeline(t, e, u, "Other Timeline")
	_, err := svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: tl.cps[2].ID, Selections: []service.Selection{{CheckpointID: &other.cps[7].ID, Kinds: []domain.KnowledgeKind{domain.KindEvidence}}}})
	wantKind(t, err, domain.KindInvalid)
	byItem := try(svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: tl.cps[1].ID, Selections: []service.Selection{{ItemIDs: []uuid.UUID{tl.d3.ID}}}})).must(t)
	if byItem.SelectedN != 1 || byItem.Branch.Name != "Fork of CP1" {
		t.Errorf("item selection: %d %q", byItem.SelectedN, byItem.Branch.Name)
	}
}

func TestMergeSelectedContextAndCompareBranches(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	tl := seedTimeline(t, e, u, "Merge Idea")
	_ = st
	fork := try(svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: tl.cps[4].ID, Name: "Premium"})).must(t).Branch
	cpsBefore := countRows(t, `SELECT count(*) FROM checkpoints WHERE idea_id = $1`, tl.idea.Idea.ID)
	merged := try(svc.MergeSelectedContext(ctx(), u.Actor(), fork.ID, []service.Selection{{ItemIDs: []uuid.UUID{tl.i1.ID, tl.e1.ID}}}, "bring research")).must(t)
	if merged.SelectedN != 2 || merged.BaseCheckpoint == nil || merged.BaseCheckpoint.Kind != domain.CheckpointMerge || merged.BaseCheckpoint.Title != "bring research" {
		t.Fatalf("merge: %+v", merged)
	}
	if countRows(t, `SELECT count(*) FROM checkpoints WHERE idea_id = $1`, tl.idea.Idea.ID) != cpsBefore+1 {
		t.Error("a merge creates exactly one merge checkpoint")
	}
	viaMerge := 0
	for _, r := range merged.Inherited {
		if r.Via == "merge" {
			viaMerge++
		}
	}
	if viaMerge != 2 {
		t.Errorf("merge inheritance = %d", viaMerge)
	}
	again := try(svc.MergeSelectedContext(ctx(), u.Actor(), fork.ID, []service.Selection{{ItemIDs: []uuid.UUID{tl.i1.ID}}}, "")).must(t)
	if again.SelectedN != 0 || again.BaseCheckpoint != nil {
		t.Errorf("merging an already present lineage is a no-op: %+v", again)
	}
	_, err := svc.MergeSelectedContext(ctx(), u.Actor(), fork.ID, nil, "")
	wantKind(t, err, domain.KindInvalid)

	cmp := try(svc.CompareBranches(ctx(), u.ID, tl.idea.Branch.ID, fork.ID, false)).must(t)
	onlyA := labels(cmp.OnlyInA)
	onlyB := labels(cmp.OnlyInB)
	if !strings.Contains(onlyA, "D2") || !strings.Contains(onlyA, "D3") || !strings.Contains(onlyA, "T1") || strings.Contains(onlyA, "E2") {
		t.Errorf("only in Main: %s", onlyA)
	}
	if !strings.Contains(onlyB, "D1") {
		t.Errorf("only in fork (D1 is live there, superseded on Main): %s", onlyB)
	}
	if cmp.Shared < 3 || cmp.Analyzer != "deterministic" {
		t.Errorf("shared = %d analyzer = %s", cmp.Shared, cmp.Analyzer)
	}
	if cmp.CommonAncestor == nil || cmp.CommonAncestor.Label != "CP4" || !strings.Contains(cmp.Narrative, "They diverged at CP4.") {
		t.Errorf("common ancestor: %+v / %q", cmp.CommonAncestor, cmp.Narrative)
	}
	other := e.MustIdea(t, u, "Unrelated", "")
	_, err = svc.CompareBranches(ctx(), u.ID, tl.idea.Branch.ID, other.Branch.ID, false)
	wantKind(t, err, domain.KindInvalid)
}

func labels(items []domain.KnowledgeItem) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label)
	}
	return strings.Join(out, ",")
}

// The divergence point of two branches must not depend on argument order, and
// must be found for nested forks and sibling forks too.
func TestCompareBranchesCommonAncestorIsSymmetric(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc := e.Svc()
	tl := seedTimeline(t, e, u, "Ancestor Idea")
	main := tl.idea.Branch.ID
	a := try(svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: tl.cps[2].ID, Name: "A"})).must(t)
	b := try(svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: tl.cps[5].ID, Name: "B"})).must(t)
	// Nested: C forks from A's base checkpoint.
	c := try(svc.ForkFromCheckpoint(ctx(), u.Actor(), service.ForkInput{CheckpointID: a.BaseCheckpoint.ID, Name: "C"})).must(t)
	tests := []struct {
		name string
		x, y uuid.UUID
		want string
	}{
		{"main vs fork", main, a.Branch.ID, "CP2"},
		{"fork vs main", a.Branch.ID, main, "CP2"},
		{"parent fork vs nested fork", a.Branch.ID, c.Branch.ID, a.BaseCheckpoint.Label},
		{"nested fork vs parent fork", c.Branch.ID, a.Branch.ID, a.BaseCheckpoint.Label},
		{"siblings", a.Branch.ID, b.Branch.ID, "CP2"},
		{"siblings reversed", b.Branch.ID, a.Branch.ID, "CP2"},
	}
	for _, tt := range tests {
		cmp := try(svc.CompareBranches(ctx(), u.ID, tt.x, tt.y, false)).must(t)
		if cmp.CommonAncestor == nil || cmp.CommonAncestor.Label != tt.want {
			t.Errorf("%s: common ancestor = %+v, want %s", tt.name, cmp.CommonAncestor, tt.want)
		}
	}
}

func TestCreateBranchFromHeadAndUpdateBranch(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	idea := e.MustIdea(t, u, "Branching Idea", "")
	e.MustRecord(t, u, decision(idea.Idea.ID, "Start small"))
	res := try(svc.CreateBranch(ctx(), u.Actor(), service.CreateBranchInput{IdeaID: idea.Idea.ID, Name: "Enterprise direction"})).must(t)
	if res.FromCheckpoint == nil || res.FromCheckpoint.Kind != domain.CheckpointAuto || res.FromCheckpoint.BranchID != idea.Branch.ID {
		t.Errorf("branching from a head snapshots it first: %+v", res.FromCheckpoint)
	}
	if res.FromCheckpointN != 1 || res.Branch.Slug != "enterprise-direction" {
		t.Errorf("branch: %+v", res.Branch)
	}
	up := try(svc.UpdateBranch(ctx(), u.Actor(), res.Branch.ID, service.UpdateBranchInput{Name: ptr("Enterprise"), Status: ptr("archived"), MakeDefault: true})).must(t)
	if up.Name != "Enterprise" || up.Status != domain.BranchArchived || !up.IsDefault {
		t.Errorf("update branch: %+v", up)
	}
	ideaNow := try(st.GetIdea(ctx(), u.ID, idea.Idea.ID)).must(t)
	if *ideaNow.DefaultBranchID != res.Branch.ID {
		t.Error("make_default must move the idea's default branch")
	}
	if old := try(st.GetBranch(ctx(), u.ID, idea.Branch.ID)).must(t); old.IsDefault {
		t.Error("the previous default branch must be unset")
	}
	_, err := svc.UpdateBranch(ctx(), u.Actor(), res.Branch.ID, service.UpdateBranchInput{Status: ptr("DELETED")})
	wantKind(t, err, domain.KindInvalid)
	tree := try(svc.GetBranchTree(ctx(), u.ID, idea.Idea.ID)).must(t)
	if len(tree.Branches) != 2 || len(tree.Checkpoints) != 2 {
		t.Errorf("tree: %d branches, %d checkpoints", len(tree.Branches), len(tree.Checkpoints))
	}
}
