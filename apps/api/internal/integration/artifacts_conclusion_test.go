package integration

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/artifacts"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

func TestGenerateArtifactWithProvenance(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	tl := seedTimeline(t, e, u, "Zoho SharePoint Extension")
	res := try(svc.GenerateArtifact(ctx(), u.Actor(), service.GenerateArtifactInput{IdeaID: tl.idea.Idea.ID, Type: "implementation prompt", Instructions: "Target Chrome"}, nil)).must(t)
	art := res.Artifact
	if art.Type != domain.ArtifactImplementationPrompt || art.Generator != "template:v1" || art.CurrentVersion != 1 || art.Status != domain.ArtifactDraft {
		t.Fatalf("offline mode must use the deterministic template: %+v", art)
	}
	if art.Title != "Zoho SharePoint Extension — Implementation Prompt" || !strings.HasPrefix(art.ContentMarkdown, "# Zoho SharePoint Extension — Implementation Prompt") {
		t.Errorf("title/markdown: %q", art.Title)
	}
	if art.BranchID == nil || *art.BranchID != tl.idea.Branch.ID || art.CheckpointID != nil {
		t.Errorf("artifact scope: %+v", art)
	}
	cited, _ := artifacts.CitedLabels(art.ContentMarkdown)
	if len(cited) == 0 {
		t.Fatal("generated artifact cites no recorded thinking")
	}
	roles := map[string]string{}
	for _, p := range res.Provenance {
		roles[p.Label] = p.Role
	}
	for _, l := range cited {
		if roles[l] != "cited" {
			t.Errorf("label %s cited in the markdown must be recorded as cited provenance (got %q)", l, roles[l])
		}
	}
	if roles["Zoho SharePoint Extension"] != "input" {
		t.Error("the idea itself is an input of the artifact")
	}
	// Live items that the template did not cite are still recorded as inputs.
	n := countRows(t, `SELECT count(*) FROM artifact_provenance p JOIN artifact_versions v ON v.id = p.artifact_version_id WHERE v.artifact_id = $1`, art.ID)
	if n < len(cited)+1 {
		t.Errorf("provenance rows = %d", n)
	}
	rel := try(st.ListRelationships(ctx(), postgres.RelationshipFilter{UserID: u.ID, EntityID: &art.ID, RelTypes: []domain.RelType{domain.RelGeneratedFrom}})).must(t)
	if len(rel) != 1 || rel[0].ToID != tl.idea.Idea.ID {
		t.Errorf("generated_from relationship: %+v", rel)
	}
	// As of a checkpoint: provenance includes the base checkpoint, content reflects that state.
	atCP := try(svc.GenerateArtifact(ctx(), u.Actor(), service.GenerateArtifactInput{IdeaID: tl.idea.Idea.ID, CheckpointID: &tl.cps[4].ID, Type: domain.ArtifactDecisionMemo}, nil)).must(t)
	if atCP.Artifact.CheckpointID == nil || *atCP.Artifact.CheckpointID != tl.cps[4].ID || atCP.Artifact.CheckpointLabel != "CP4" {
		t.Errorf("checkpoint artifact: %+v", atCP.Artifact)
	}
	if !strings.Contains(atCP.Artifact.ContentMarkdown, "We will not build a marketplace in v1 [D1]") || strings.Contains(atCP.Artifact.ContentMarkdown, "verified riders") {
		t.Errorf("an artifact as of CP4 must reflect CP4, not later decisions:\n%s", atCP.Artifact.ContentMarkdown)
	}
	hasBase := false
	for _, p := range atCP.Provenance {
		if p.Role == "base_checkpoint" && p.Label == "CP4" {
			hasBase = true
		}
	}
	if !hasBase {
		t.Errorf("base checkpoint missing from provenance: %+v", atCP.Provenance)
	}
	// Validation.
	_, err := svc.GenerateArtifact(ctx(), u.Actor(), service.GenerateArtifactInput{IdeaID: tl.idea.Idea.ID, Type: "poem"}, nil)
	wantKind(t, err, domain.KindInvalid)
	_, err = svc.GenerateArtifact(ctx(), u.Actor(), service.GenerateArtifactInput{IdeaID: uuid.New(), Type: domain.ArtifactActionPlan}, nil)
	wantKind(t, err, domain.KindNotFound)
	// Explanation from provenance (deterministic analyzer offline).
	ex := try(svc.ExplainArtifact(ctx(), u.ID, atCP.Artifact.ID, "why no marketplace?", 0)).must(t)
	if ex.Analyzer != "deterministic" || !strings.Contains(ex.Answer, "[D1]") || ex.Checkpoint == nil || ex.Checkpoint.Label != "CP4" {
		t.Errorf("explain artifact: %+v", ex)
	}
}

func TestArtifactVersioningAndRegeneration(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	tl := seedTimeline(t, e, u, "Versioned Artifact Idea")
	created := try(svc.CreateArtifact(ctx(), u.Actor(), service.CreateArtifactInput{IdeaID: tl.idea.Idea.ID, Type: "brief", Title: "My brief",
		ContentMarkdown: "# My brief\n\nBased on [D2] and [I1]. Unknown [D99] is ignored."})).must(t)
	if created.Artifact.Type != domain.ArtifactProductBrief || created.Artifact.Generator != "user" {
		t.Errorf("hand-written artifact: %+v", created.Artifact)
	}
	cited := map[string]bool{}
	for _, p := range created.Provenance {
		if p.Role == "cited" {
			cited[p.Label] = true
		}
	}
	if !cited["D2"] || !cited["I1"] || cited["D99"] {
		t.Errorf("hand-written provenance resolves only real labels: %v", cited)
	}
	v1 := created.Artifact.ContentMarkdown
	up := try(svc.UpdateArtifact(ctx(), u.Actor(), created.Artifact.ID, service.UpdateArtifactInput{ContentMarkdown: ptr("# My brief\n\nNow citing [D3]."), ChangeNote: "tighter"})).must(t)
	if up.Artifact.CurrentVersion != 2 || up.Version.Version != 2 || up.Version.ChangeNote != "tighter" {
		t.Fatalf("update must create v2: %+v", up.Version)
	}
	versions := try(st.ListArtifactVersions(ctx(), u.ID, created.Artifact.ID)).must(t)
	if len(versions) != 2 || versions[1].Version != 1 || versions[1].ContentMarkdown != v1 {
		t.Errorf("v1 must be kept unchanged: %+v", versions)
	}
	p1 := try(st.ArtifactProvenance(ctx(), u.ID, created.Artifact.ID, 1)).must(t)
	p2 := try(st.ArtifactProvenance(ctx(), u.ID, created.Artifact.ID, 0)).must(t)
	if !hasLabel(p1, "D2") || hasLabel(p1, "D3") || !hasLabel(p2, "D3") || hasLabel(p2, "D2") {
		t.Errorf("provenance is per version: v1=%v current=%v", p1, p2)
	}
	_, err := svc.UpdateArtifact(ctx(), u.Actor(), created.Artifact.ID, service.UpdateArtifactInput{ContentMarkdown: ptr("# My brief\n\nNow citing [D3].")})
	wantKind(t, err, domain.KindInvalid) // no changes
	_, err = svc.UpdateArtifact(ctx(), u.Actor(), created.Artifact.ID, service.UpdateArtifactInput{Status: ptr("published")})
	wantKind(t, err, domain.KindInvalid)
	fin := try(svc.UpdateArtifact(ctx(), u.Actor(), created.Artifact.ID, service.UpdateArtifactInput{Status: ptr("final")})).must(t)
	if fin.Artifact.Status != domain.ArtifactFinal || fin.Artifact.CurrentVersion != 2 {
		t.Errorf("status change must not create a version: %+v", fin.Artifact)
	}
	regen := try(svc.RegenerateArtifact(ctx(), u.Actor(), created.Artifact.ID, service.RegenerateArtifactInput{CheckpointID: &tl.cps[2].ID, Instructions: "shorter"}, nil)).must(t)
	if regen.Artifact.CurrentVersion != 3 || regen.Version.Generator != "template:v1" || !strings.HasPrefix(regen.Version.ChangeNote, "Regenerated: shorter") ||
		regen.Artifact.CheckpointID == nil || *regen.Artifact.CheckpointID != tl.cps[2].ID {
		t.Errorf("regenerate: %+v %+v", regen.Artifact, regen.Version)
	}
	if !strings.HasPrefix(regen.Artifact.ContentMarkdown, "# My brief") {
		t.Errorf("regenerated artifact keeps its title: %q", firstLineOf(regen.Artifact.ContentMarkdown))
	}
	if n := len(try(st.ListArtifactVersions(ctx(), u.ID, created.Artifact.ID)).must(t)); n != 3 {
		t.Errorf("versions after regenerate = %d", n)
	}
	// Destructive deletion removes every version.
	if err := svc.DeleteArtifact(ctx(), u.Actor(), created.Artifact.ID); err != nil {
		t.Fatal(err)
	}
	if countRows(t, `SELECT count(*) FROM artifact_versions WHERE artifact_id = $1`, created.Artifact.ID) != 0 {
		t.Error("artifact versions must be deleted with the artifact")
	}
	wantKind(t, svc.DeleteArtifact(ctx(), u.Actor(), created.Artifact.ID), domain.KindNotFound)
}

func hasLabel(p []domain.ProvenanceEntry, label string) bool {
	for _, x := range p {
		if x.Label == label {
			return true
		}
	}
	return false
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestBuildContextPack(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	tl := seedTimeline(t, e, u, "Context Pack Idea")
	pack := try(svc.BuildContextPack(ctx(), u.Actor(), service.BuildContextPackInput{IdeaID: tl.idea.Idea.ID, Objective: "Decide pricing"})).must(t)
	md := pack.ContentMarkdown
	for _, want := range []string{"# IdeaVault Context Pack: Context Pack Idea", "## Current objective\n\nDecide pricing", "**[D2]** Build a marketplace for verified riders",
		"**[D3]** Charge organizers, not riders", "## How my thinking changed", "~~[D1] We will not build a marketplace in v1~~ — superseded",
		"- [ ] **[T1]** Prototype the trip planner", "## Origin (in my own words)"} {
		if !strings.Contains(md, want) {
			t.Errorf("context pack markdown missing %q", want)
		}
	}
	if pack.Title != "Context Pack Idea — context pack (Main)" || pack.TokenEstimate <= 0 || pack.BranchID == nil {
		t.Errorf("pack metadata: %+v", pack)
	}
	j := pack.ContentJSON
	if j["format"] != "ideavault.context_pack.v1" || j["objective"] != "Decide pricing" {
		t.Errorf("pack json: %v", j)
	}
	if ds, _ := j["decisions"].([]map[string]any); len(ds) != 2 {
		t.Errorf("json decisions = %d", len(ds))
	}
	stored := try(st.GetContextPack(ctx(), u.ID, pack.ID)).must(t)
	if stored.ContentMarkdown != md || stored.ContentJSON["format"] != "ideavault.context_pack.v1" {
		t.Error("stored pack differs")
	}
	if decs, _ := stored.ContentJSON["decisions"].([]any); len(decs) != 2 {
		t.Errorf("stored json decisions = %v", stored.ContentJSON["decisions"])
	}
	atCP := try(svc.BuildContextPack(ctx(), u.Actor(), service.BuildContextPackInput{IdeaID: tl.idea.Idea.ID, CheckpointID: &tl.cps[1].ID})).must(t)
	if !strings.HasSuffix(atCP.Title, "(CP1)") || strings.Contains(atCP.ContentMarkdown, "verified riders") || !strings.Contains(atCP.ContentMarkdown, "Continue developing this idea") {
		t.Errorf("pack at CP1: %q", atCP.Title)
	}
	list := try(st.ListContextPacks(ctx(), u.ID, &tl.idea.Idea.ID, 10)).must(t)
	if len(list) != 2 || list[0].ContentMarkdown != "" {
		t.Errorf("list packs (without heavy content): %d", len(list))
	}
}

func TestConcludeIdeaFlows(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	tl := seedTimeline(t, e, u, "Conclude Idea")
	cpsBefore := countRows(t, `SELECT count(*) FROM checkpoints WHERE idea_id = $1`, tl.idea.Idea.ID)
	kBefore := countRows(t, `SELECT count(*) FROM knowledge_items WHERE idea_id = $1`, tl.idea.Idea.ID)
	res := try(svc.ConcludeIdea(ctx(), u.Actor(), service.ConcludeInput{IdeaID: tl.idea.Idea.ID, Outcome: domain.OutcomeActionPlan, Note: "Ship the planner",
		GenerateArtifacts: []domain.ArtifactType{domain.ArtifactActionPlan}})).must(t)
	if res.Idea.Status != domain.StatusConcluded || res.Idea.Outcome == nil || *res.Idea.Outcome != domain.OutcomeActionPlan || res.Idea.ConcludedAt == nil {
		t.Errorf("concluded idea: %+v", res.Idea)
	}
	if res.Checkpoint.Kind != domain.CheckpointConclusion || res.Checkpoint.Number != cpsBefore+1 || res.Checkpoint.Title != "Conclusion: action plan" {
		t.Errorf("final checkpoint: %+v", res.Checkpoint)
	}
	c := res.Conclusion
	if c.PreviousStatus != domain.StatusActive || c.NewStatus != domain.StatusConcluded || !strings.Contains(c.Decisions, "[D3] Charge organizers") ||
		!strings.Contains(c.Learned, "[I1]") || !strings.Contains(c.Unresolved, "[Q1]") || strings.Contains(c.Decisions, "[D1]") {
		t.Errorf("conclusion summaries: %+v", c)
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].Artifact.Type != domain.ArtifactActionPlan || *res.Artifacts[0].Artifact.CheckpointID != res.Checkpoint.ID {
		t.Errorf("conclusion artifact: %+v", res.Artifacts)
	}
	if countRows(t, `SELECT count(*) FROM knowledge_items WHERE idea_id = $1`, tl.idea.Idea.ID) != kBefore {
		t.Error("concluding must never delete knowledge")
	}
	br := try(st.GetBranch(ctx(), u.ID, tl.idea.Branch.ID)).must(t)
	if br.Status != domain.BranchConcluded {
		t.Errorf("branch status = %s", br.Status)
	}
	if cs := try(st.ListConclusions(ctx(), u.ID, tl.idea.Idea.ID)).must(t); len(cs) != 1 {
		t.Errorf("conclusion records = %d", len(cs))
	}
	// Status mapping for the other outcomes.
	for outcome, want := range map[domain.Outcome]domain.IdeaStatus{domain.OutcomeImplement: domain.StatusReadyToImplement, domain.OutcomePark: domain.StatusParked,
		domain.OutcomeAbandon: domain.StatusAbandoned, domain.OutcomeDecision: domain.StatusDecided, domain.OutcomeReference: domain.StatusConcluded} {
		idea := e.MustIdea(t, u, "Outcome "+string(outcome), "")
		r := try(svc.ConcludeIdea(ctx(), u.Actor(), service.ConcludeInput{IdeaID: idea.Idea.ID, Outcome: outcome})).must(t)
		if r.Idea.Status != want {
			t.Errorf("%s → %s, want %s", outcome, r.Idea.Status, want)
		}
	}
	// MERGE requires a target, cannot target itself, records merged_into.
	src := e.MustIdea(t, u, "Merge source", "")
	dst := e.MustIdea(t, u, "Merge target", "")
	_, err := svc.ConcludeIdea(ctx(), u.Actor(), service.ConcludeInput{IdeaID: src.Idea.ID, Outcome: domain.OutcomeMerge})
	wantKind(t, err, domain.KindInvalid)
	_, err = svc.ConcludeIdea(ctx(), u.Actor(), service.ConcludeInput{IdeaID: src.Idea.ID, Outcome: domain.OutcomeMerge, MergeIntoIdeaID: &src.Idea.ID})
	wantKind(t, err, domain.KindInvalid)
	m := try(svc.ConcludeIdea(ctx(), u.Actor(), service.ConcludeInput{IdeaID: src.Idea.ID, Outcome: domain.OutcomeMerge, MergeIntoIdeaID: &dst.Idea.ID})).must(t)
	if m.Idea.Status != domain.StatusMerged || m.Idea.MergedIntoIdeaID == nil || *m.Idea.MergedIntoIdeaID != dst.Idea.ID {
		t.Errorf("merged idea: %+v", m.Idea)
	}
	rels := try(st.ListRelationships(ctx(), postgres.RelationshipFilter{UserID: u.ID, EntityID: &src.Idea.ID, RelTypes: []domain.RelType{domain.RelMergedInto}})).must(t)
	if len(rels) != 1 || rels[0].ToID != dst.Idea.ID {
		t.Errorf("merged_into relationship: %+v", rels)
	}
	_, err = svc.ConcludeIdea(ctx(), u.Actor(), service.ConcludeInput{IdeaID: src.Idea.ID, Outcome: domain.OutcomeReference})
	wantKind(t, err, domain.KindConflict) // MERGED is terminal
	_, err = svc.ReopenIdea(ctx(), u.Actor(), src.Idea.ID, "")
	wantKind(t, err, domain.KindInvalid)
	_, err = svc.ConcludeIdea(ctx(), u.Actor(), service.ConcludeInput{IdeaID: dst.Idea.ID, Outcome: "SHIP"})
	wantKind(t, err, domain.KindInvalid)
	// History is preserved and the idea can be reopened (except when merged).
	re := try(svc.ReopenIdea(ctx(), u.Actor(), tl.idea.Idea.ID, "new evidence")).must(t)
	if re.Status != domain.StatusActive {
		t.Errorf("reopen: %s", re.Status)
	}
	archive := try(svc.Archive(ctx(), u.ID)).must(t)
	if len(archive) != 4 { // parked, abandoned, concluded (reference), merged
		t.Errorf("archive = %d ideas", len(archive))
	}
}
