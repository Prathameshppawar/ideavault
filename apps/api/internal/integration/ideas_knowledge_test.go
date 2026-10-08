package integration

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

func TestCreateIdeaPreservesOriginConversation(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	st := e.Store()
	conv := &domain.Conversation{UserID: u.ID, Title: "Native chat"}
	if err := st.CreateConversation(ctx(), conv); err != nil {
		t.Fatal(err)
	}
	msg := &domain.Message{UserID: u.ID, ConversationID: conv.ID, Role: domain.RoleUser, Content: "I have an idea for a biker community platform."}
	if err := st.AppendMessage(ctx(), msg); err != nil {
		t.Fatal(err)
	}
	res := try(e.Svc().CreateIdea(ctx(), u.Actor(), service.CreateIdeaInput{OriginText: "I have an idea for a biker community platform.\nMore detail.",
		Tags: []string{" Mobility ", "mobility", "Social"}, ConversationID: &conv.ID})).must(t)
	idea, br := res.Idea, res.Branch
	if idea.Title != "I have an idea for a biker community platform." {
		t.Errorf("title defaults to the first line of the origin: %q", idea.Title)
	}
	if idea.Status != domain.StatusExploring || idea.Version != 1 || strings.Join(idea.Tags, ",") != "mobility,social" {
		t.Errorf("idea defaults: %+v", idea)
	}
	if br.Name != "Main" || !br.IsDefault || idea.DefaultBranchID == nil || *idea.DefaultBranchID != br.ID {
		t.Errorf("default branch: %+v", br)
	}
	if idea.OriginConversationID == nil || *idea.OriginConversationID != conv.ID {
		t.Fatal("origin conversation not linked")
	}
	gotConv := try(st.GetConversation(ctx(), u.ID, conv.ID)).must(t)
	if gotConv.IdeaID == nil || *gotConv.IdeaID != idea.ID || gotConv.BranchID == nil || *gotConv.BranchID != br.ID {
		t.Errorf("conversation must be attached to the idea's Main branch: %+v", gotConv)
	}
	stored := try(e.Svc().GetIdea(ctx(), u.ID, idea.ID)).must(t)
	if stored.OriginConversationID == nil || stored.Stats == nil || stored.Stats.Branches != 1 || stored.Stats.Conversations != 1 {
		t.Errorf("stored idea/stats: %+v %+v", stored, stored.Stats)
	}
	versions := try(st.ListIdeaVersions(ctx(), u.ID, idea.ID)).must(t)
	if len(versions) != 1 || versions[0].Version != 1 || versions[0].ChangedFields[0] != "created" || versions[0].CreatedBy != domain.ActorUser {
		t.Errorf("version 1 must be recorded: %+v", versions)
	}
	acts := try(st.ListActivity(ctx(), postgres.ActivityFilter{UserID: u.ID, IdeaID: &idea.ID})).must(t)
	if len(acts) != 1 || acts[0].EventType != "idea.created" {
		t.Errorf("activity: %+v", acts)
	}
	// A conversation that already belongs to another idea is referenced, not moved.
	second := try(e.Svc().CreateIdea(ctx(), u.Actor(), service.CreateIdeaInput{Title: "Spin-off", ConversationID: &conv.ID})).must(t)
	gotConv = try(st.GetConversation(ctx(), u.ID, conv.ID)).must(t)
	if *gotConv.IdeaID != idea.ID {
		t.Error("conversation moved to the second idea")
	}
	linked := try(st.BranchLinkedIDs(ctx(), second.Branch.ID, domain.EntityConversation)).must(t)
	if len(linked) != 1 || linked[0] != conv.ID || *second.Idea.OriginConversationID != conv.ID {
		t.Errorf("second idea must reference the conversation: %v", linked)
	}
	convs := try(st.ListConversations(ctx(), postgres.ConversationFilter{UserID: u.ID, BranchID: &second.Branch.ID, IncludeLinked: true})).must(t)
	if len(convs) != 1 {
		t.Errorf("linked conversations are listed for the branch: %d", len(convs))
	}
}

func TestCreateIdeaValidation(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	tests := []struct {
		name string
		in   service.CreateIdeaInput
		kind domain.ErrorKind
	}{
		{"no title or origin", service.CreateIdeaInput{}, domain.KindInvalid},
		{"blank", service.CreateIdeaInput{Title: "   ", OriginText: "  "}, domain.KindInvalid},
		{"too long", service.CreateIdeaInput{Title: strings.Repeat("x", 301)}, domain.KindInvalid},
		{"bad status", service.CreateIdeaInput{Title: "x", Status: "SHIPPED"}, domain.KindInvalid},
		{"foreign conversation", service.CreateIdeaInput{Title: "x", ConversationID: ptr(e.NewUser(t).ID)}, domain.KindNotFound},
	}
	for _, tt := range tests {
		_, err := e.Svc().CreateIdea(ctx(), u.Actor(), tt.in)
		if domain.KindOf(err) != tt.kind {
			t.Errorf("%s: got %v, want %s", tt.name, err, tt.kind)
		}
	}
	ok := try(e.Svc().CreateIdea(ctx(), u.Actor(), service.CreateIdeaInput{Title: "Parked from start", Status: "parked"})).must(t)
	if ok.Idea.Status != domain.StatusParked {
		t.Errorf("explicit status: %s", ok.Idea.Status)
	}
	_, total, _ := e.Store().ListIdeas(ctx(), postgres.IdeaFilter{UserID: u.ID})
	if total != 1 {
		t.Errorf("failed creations must not leave rows behind: %d ideas", total)
	}
}

func TestUpdateIdeaVersionsAndTransitions(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	idea := e.MustIdea(t, u, "Versioned Idea", "origin")
	svc := e.Svc()
	up := try(svc.UpdateIdea(ctx(), u.Actor(), idea.Idea.ID, service.UpdateIdeaInput{Title: ptr("Versioned Idea v2"), Summary: ptr("now with a summary"), Reason: "clarified"})).must(t)
	if up.Version != 2 || up.Title != "Versioned Idea v2" {
		t.Errorf("update: %+v", up)
	}
	same := try(svc.UpdateIdea(ctx(), u.Actor(), idea.Idea.ID, service.UpdateIdeaInput{Title: ptr("Versioned Idea v2")})).must(t)
	if same.Version != 2 {
		t.Error("a no-op update must not create a version")
	}
	try(svc.UpdateIdea(ctx(), u.Actor(), idea.Idea.ID, service.UpdateIdeaInput{Status: ptr("concluded")})).must(t)
	_, err := svc.UpdateIdea(ctx(), u.Actor(), idea.Idea.ID, service.UpdateIdeaInput{Status: ptr("PARKED")})
	wantKind(t, err, domain.KindInvalid) // CONCLUDED → PARKED is not allowed
	_, err = svc.UpdateIdea(ctx(), u.Actor(), idea.Idea.ID, service.UpdateIdeaInput{Status: ptr("MERGED")})
	wantKind(t, err, domain.KindInvalid) // merging goes through conclude
	_, err = svc.UpdateIdea(ctx(), u.Actor(), idea.Idea.ID, service.UpdateIdeaInput{Title: ptr("")})
	wantKind(t, err, domain.KindInvalid)
	re := try(svc.ReopenIdea(ctx(), u.Actor(), idea.Idea.ID, "")).must(t)
	if re.Status != domain.StatusActive || re.ConcludedAt != nil {
		t.Errorf("reopen: %+v", re)
	}
	versions := try(e.Store().ListIdeaVersions(ctx(), u.ID, idea.Idea.ID)).must(t)
	if len(versions) != 4 {
		t.Fatalf("expected 4 versions (create, edit, conclude, reopen), got %d", len(versions))
	}
	if versions[1].ChangeReason != "clarified" || strings.Join(versions[1].ChangedFields, ",") != "title,summary" || versions[0].Title != "Versioned Idea" {
		t.Errorf("history must keep every past identity: %+v", versions)
	}
	acts := try(e.Store().ListActivity(ctx(), postgres.ActivityFilter{UserID: u.ID, IdeaID: &idea.Idea.ID, EventTypes: []string{"idea.status_changed"}})).must(t)
	if len(acts) != 2 {
		t.Errorf("status changes are logged: %d", len(acts))
	}
}

func TestRecordKnowledgeSupersedeAndHistory(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	idea := e.MustIdea(t, u, "Biker Platform", "riders and trips")
	d1 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "We will not build a marketplace in v1",
		Rationale: "moderation is expensive", Alternatives: []domain.Alternative{{Option: "Escrow marketplace", ReasonRejected: "compliance"}, {Option: " "}}})
	if d1.Label != "D1" || d1.Status != "ACTIVE" || d1.Origin != domain.OriginSource || d1.Decision.Rationale != "moderation is expensive" || len(d1.Decision.Alternatives) != 1 {
		t.Fatalf("D1 = %+v %+v", d1, d1.Decision)
	}
	afterFirst := try(st.GetIdea(ctx(), u.ID, idea.Idea.ID)).must(t)
	if afterFirst.Status != domain.StatusActive || afterFirst.Version != 2 {
		t.Errorf("the first decision moves an exploring idea to ACTIVE: %s v%d", afterFirst.Status, afterFirst.Version)
	}
	d2 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "Build a marketplace for verified riders in v2",
		Rationale: "verification solves moderation", Supersedes: &d1.ID})
	old := try(st.GetKnowledge(ctx(), u.ID, d1.ID)).must(t)
	if old.Status != "SUPERSEDED" || old.SupersededByID == nil || *old.SupersededByID != d2.ID {
		t.Errorf("old decision must be SUPERSEDED by D2: %+v", old)
	}
	if old.Statement != d1.Statement || old.Decision.Rationale != d1.Decision.Rationale {
		t.Error("superseding must never rewrite the old statement")
	}
	if d2.Label != "D2" || d2.LineageID == d1.LineageID {
		t.Errorf("a superseding item is a new item: %s", d2.Label)
	}
	rels := try(st.ListRelationships(ctx(), postgres.RelationshipFilter{UserID: u.ID, EntityID: &d1.ID, RelTypes: []domain.RelType{domain.RelSupersedes}})).must(t)
	if len(rels) != 1 || rels[0].FromID != d2.ID || rels[0].ToID != d1.ID {
		t.Errorf("supersedes relationship: %+v", rels)
	}
	chain := try(st.SupersessionChain(ctx(), u.ID, d2.ID)).must(t)
	if len(chain) != 2 || chain[0].ID != d1.ID || chain[1].ID != d2.ID {
		t.Errorf("history must be recoverable oldest-first: %+v", chain)
	}
	ex := try(svc.ExplainKnowledge(ctx(), u.ID, d1.ID)).must(t)
	if len(ex.Chain) != 2 {
		t.Errorf("explaining the old item shows its successor: %+v", ex.Chain)
	}
	// Already superseded → conflict; wrong kind or idea → invalid.
	_, err := svc.RecordKnowledge(ctx(), u.Actor(), service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "again", Supersedes: &d1.ID})
	wantKind(t, err, domain.KindConflict)
	_, err = svc.RecordKnowledge(ctx(), u.Actor(), service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindInsight, Statement: "x", Supersedes: &d2.ID})
	wantKind(t, err, domain.KindInvalid)
	other := e.MustIdea(t, u, "Other", "")
	_, err = svc.RecordKnowledge(ctx(), u.Actor(), service.RecordKnowledgeInput{IdeaID: other.Idea.ID, Kind: domain.KindDecision, Statement: "x", Supersedes: &d2.ID})
	wantKind(t, err, domain.KindInvalid)

	// Reversal.
	d3 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "No marketplace at all", Supersedes: &d2.ID, Reverses: true})
	rev := try(st.GetKnowledge(ctx(), u.ID, d2.ID)).must(t)
	if rev.Status != "REVERSED" || d3.Decision.ReversesID == nil || *d3.Decision.ReversesID != d2.ID {
		t.Errorf("reversal: %s %+v", rev.Status, d3.Decision)
	}
	acts := try(st.ListActivity(ctx(), postgres.ActivityFilter{UserID: u.ID, IdeaID: &idea.Idea.ID, EventTypes: []string{"decision.superseded", "decision.reversed"}})).must(t)
	if len(acts) != 2 {
		t.Errorf("supersession/reversal activity: %d", len(acts))
	}
	full := try(st.SupersessionChain(ctx(), u.ID, d1.ID)).must(t)
	if len(full) != 3 {
		t.Errorf("full chain D1→D2→D3 expected, got %d", len(full))
	}
	// Status changes never accept SUPERSEDED directly and never touch superseded items.
	_, err = svc.UpdateKnowledgeStatus(ctx(), u.Actor(), d3.ID, service.UpdateKnowledgeStatusInput{Status: "SUPERSEDED"})
	wantKind(t, err, domain.KindInvalid)
	_, err = svc.UpdateKnowledgeStatus(ctx(), u.Actor(), d1.ID, service.UpdateKnowledgeStatusInput{Status: "REJECTED"})
	wantKind(t, err, domain.KindConflict)
	_, err = svc.UpdateKnowledgeStatus(ctx(), u.Actor(), d3.ID, service.UpdateKnowledgeStatusInput{Status: "OPEN"})
	wantKind(t, err, domain.KindInvalid)
}

func TestKnowledgeKindsStatusesAndEvidence(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	idea := e.MustIdea(t, u, "Kinds Idea", "")
	id := idea.Idea.ID
	a1 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: "assumptions", Statement: "Riders pay for route packs", Risk: "high", ValidationMethod: "pre-sales"})
	if a1.Kind != domain.KindAssumption || a1.Status != "UNVALIDATED" || a1.Assumption.Risk != "HIGH" || a1.Assumption.ValidationMethod != "pre-sales" {
		t.Errorf("assumption: %+v %+v", a1, a1.Assumption)
	}
	e1 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindEvidence, Statement: "12 of 20 riders pre-ordered", Stance: "supports", Strength: "strong",
		URL: "https://example.com/survey", TargetItemID: &a1.ID})
	e2 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindEvidence, Statement: "Competitor packs flopped", Stance: "CHALLENGES", TargetItemID: &a1.ID})
	ex := try(svc.ExplainKnowledge(ctx(), u.ID, a1.ID)).must(t)
	if len(ex.Supporting) != 1 || ex.Supporting[0].ID != e1.ID || len(ex.Challenging) != 1 || ex.Challenging[0].ID != e2.ID {
		t.Errorf("evidence links: supporting=%d challenging=%d", len(ex.Supporting), len(ex.Challenging))
	}
	v := try(svc.UpdateKnowledgeStatus(ctx(), u.Actor(), a1.ID, service.UpdateKnowledgeStatusInput{Status: "validated", Note: "pre-sales worked"})).must(t)
	if v.Status != "VALIDATED" || v.Assumption.ValidatedAt == nil {
		t.Errorf("validated assumption: %+v", v.Assumption)
	}
	q1 := e.MustRecord(t, u, knowledge(id, domain.KindQuestion, "Should trips be public by default?"))
	_, err := svc.UpdateKnowledgeStatus(ctx(), u.Actor(), q1.ID, service.UpdateKnowledgeStatusInput{Status: "ANSWERED"})
	wantKind(t, err, domain.KindInvalid) // needs an answer
	ans := try(svc.UpdateKnowledgeStatus(ctx(), u.Actor(), q1.ID, service.UpdateKnowledgeStatusInput{Status: "ANSWERED", Answer: "Private by default", ByItem: &e1.ID})).must(t)
	if ans.Question.Answer != "Private by default" || ans.Question.AnsweredAt == nil || ans.Question.AnsweredByItemID == nil {
		t.Errorf("answer: %+v", ans.Question)
	}
	t1 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: "next_step", Statement: "Prototype the trip planner", Priority: "HIGH"})
	done := try(svc.UpdateKnowledgeStatus(ctx(), u.Actor(), t1.ID, service.UpdateKnowledgeStatusInput{Status: "DONE"})).must(t)
	if done.Status != "DONE" || done.Action.CompletedAt == nil || t1.Label != "T1" {
		t.Errorf("action: %+v", done.Action)
	}
	i1 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: id, Kind: domain.KindInsight, Statement: "Organizers drive adoption", Importance: "high",
		Related: []service.RelatedLink{{ItemID: e1.ID, RelType: domain.RelDerivedFrom}, {ItemID: q1.ID}}})
	rels := try(st.ListRelationships(ctx(), postgres.RelationshipFilter{UserID: u.ID, EntityID: &i1.ID})).must(t)
	if len(rels) != 2 {
		t.Errorf("related links: %+v", rels)
	}
	// Validation matrix.
	bad := []service.RecordKnowledgeInput{
		{IdeaID: id, Kind: "opinion", Statement: "x"},
		{IdeaID: id, Kind: domain.KindDecision, Statement: "  "},
		{IdeaID: id, Kind: domain.KindDecision, Statement: strings.Repeat("x", 4001)},
		{IdeaID: id, Kind: domain.KindDecision, Statement: "x", Status: "OPEN"},
		{IdeaID: id, Kind: domain.KindDecision, Statement: "x", Origin: "GUESS"},
		{IdeaID: id, Kind: domain.KindDecision, Statement: "x", Confidence: ptr(float32(1.5))},
		{IdeaID: id, Kind: domain.KindAssumption, Statement: "x", Risk: "EXTREME"},
		{IdeaID: id, Kind: domain.KindEvidence, Statement: "x", Stance: "MAYBE"},
		{IdeaID: id, Kind: domain.KindEvidence, Statement: "x", Strength: "HUGE"},
		{IdeaID: id, Kind: domain.KindEvidence, Statement: "x", URL: "javascript:alert(1)"},
		{IdeaID: id, Kind: domain.KindInsight, Statement: "x", Importance: "CRITICAL"},
		{IdeaID: id, Kind: domain.KindAction, Statement: "x", Priority: "URGENT"},
		{IdeaID: id, Kind: domain.KindEvidence, Statement: "x", TargetItemID: ptr(idea.Branch.ID)},
		{IdeaID: id, Kind: domain.KindInsight, Statement: "x", Related: []service.RelatedLink{{ItemID: e1.ID, RelType: "loves"}}},
		{IdeaID: id, Kind: domain.KindInsight, Statement: "x", SourceMessageID: ptr(idea.Idea.ID)},
	}
	before := countRows(t, `SELECT count(*) FROM knowledge_items WHERE idea_id = $1`, id)
	for i, in := range bad {
		if _, err := svc.RecordKnowledge(ctx(), u.Actor(), in); domain.KindOf(err) != domain.KindInvalid {
			t.Errorf("bad input %d (%s %q): expected invalid, got %v", i, in.Kind, trimForMsg(in.Statement), err)
		}
	}
	if after := countRows(t, `SELECT count(*) FROM knowledge_items WHERE idea_id = $1`, id); after != before {
		t.Errorf("failed records left %d rows behind", after-before)
	}
}

func trimForMsg(s string) string {
	if len(s) > 20 {
		return s[:20] + "…"
	}
	return s
}

func TestProposalsAndSourceProvenance(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	idea := e.MustIdea(t, u, "Provenance Idea", "")
	conv := &domain.Conversation{UserID: u.ID, IdeaID: &idea.Idea.ID, BranchID: &idea.Branch.ID, Title: "Imported", Origin: domain.OriginImported, Provider: "chatgpt"}
	if err := st.CreateConversation(ctx(), conv); err != nil {
		t.Fatal(err)
	}
	msg := &domain.Message{UserID: u.ID, ConversationID: conv.ID, Role: domain.RoleUser, Content: "We decided to launch in Pune first because our riders are there.", Untrusted: true}
	if err := st.AppendMessage(ctx(), msg); err != nil {
		t.Fatal(err)
	}
	prop := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindDecision, Statement: "Launch in Pune first", Proposed: true, SourceMessageID: &msg.ID})
	if prop.ReviewState != domain.ReviewProposed || prop.SourceConversationID == nil || *prop.SourceConversationID != conv.ID || !strings.Contains(prop.SourceExcerpt, "Pune") {
		t.Errorf("proposal provenance: %+v", prop)
	}
	if s := try(st.GetIdea(ctx(), u.ID, idea.Idea.ID)).must(t); s.Status != domain.StatusExploring {
		t.Error("a PROPOSED decision must not activate the idea")
	}
	ov := try(svc.IdeaOverview(ctx(), u.ID, idea.Idea.ID, nil)).must(t)
	if len(ov.Proposed) != 1 || len(ov.Knowledge[domain.KindDecision]) != 0 {
		t.Errorf("proposals are kept separate from durable knowledge: proposed=%d decisions=%d", len(ov.Proposed), len(ov.Knowledge[domain.KindDecision]))
	}
	accepted := try(svc.ReviewKnowledge(ctx(), u.Actor(), []uuid.UUID{prop.ID}, true)).must(t)
	if len(accepted) != 1 || accepted[0].ReviewState != domain.ReviewAccepted {
		t.Errorf("accept: %+v", accepted)
	}
	_, err := svc.ReviewKnowledge(ctx(), u.Actor(), []uuid.UUID{prop.ID}, false)
	wantKind(t, err, domain.KindConflict)
	rej := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: idea.Idea.ID, Kind: domain.KindInsight, Statement: "Noise", Proposed: true})
	try(svc.ReviewKnowledge(ctx(), u.Actor(), []uuid.UUID{rej.ID}, false)).must(t)
	if got := try(st.GetKnowledge(ctx(), u.ID, rej.ID)).must(t); got.ReviewState != domain.ReviewRejected || got.IsLive() {
		t.Errorf("rejected proposal: %+v", got)
	}
	ex := try(svc.ExplainKnowledge(ctx(), u.ID, prop.ID)).must(t)
	if ex.SourceMessage == nil || ex.SourceMessage.ID != msg.ID || !ex.SourceMessage.Untrusted || ex.Conversation == nil || len(ex.SourceWindow) != 1 {
		t.Errorf("explanation must include the untrusted source message: %+v", ex.SourceMessage)
	}
}
