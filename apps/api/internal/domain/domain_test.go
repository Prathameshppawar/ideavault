package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from, to IdeaStatus
		want     bool
	}{
		{StatusExploring, StatusExploring, true},
		{StatusExploring, StatusActive, true},
		{StatusExploring, StatusConcluded, true},
		{StatusExploring, StatusMerged, true},
		{StatusActive, StatusDecided, true},
		{StatusDecided, StatusReadyToImplement, true},
		{StatusReadyToImplement, StatusConcluded, true},
		{StatusConcluded, StatusActive, true},
		{StatusConcluded, StatusExploring, true},
		{StatusConcluded, StatusParked, false},
		{StatusConcluded, StatusMerged, false},
		{StatusParked, StatusAbandoned, true},
		{StatusParked, StatusMerged, true},
		{StatusParked, StatusConcluded, false},
		{StatusAbandoned, StatusActive, true},
		{StatusAbandoned, StatusConcluded, false},
		{StatusAbandoned, StatusMerged, false},
		// MERGED is terminal: nothing leaves it (except the identity transition).
		{StatusMerged, StatusMerged, true},
		{StatusMerged, StatusExploring, false},
		{StatusMerged, StatusActive, false},
		{StatusMerged, StatusConcluded, false},
		{StatusMerged, StatusParked, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s->%s", tt.from, tt.to), func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestMergedIsTerminal(t *testing.T) {
	for _, to := range AllIdeaStatuses {
		if to != StatusMerged && CanTransition(StatusMerged, to) {
			t.Errorf("MERGED must be terminal, but can move to %s", to)
		}
	}
	// Every non-merged status can always be reopened to EXPLORING or ACTIVE.
	for _, from := range AllIdeaStatuses {
		if from == StatusMerged {
			continue
		}
		if !CanTransition(from, StatusExploring) || !CanTransition(from, StatusActive) {
			t.Errorf("%s should be reopenable", from)
		}
	}
}

func TestIdeaStatusValidAndOpen(t *testing.T) {
	for _, s := range AllIdeaStatuses {
		if !s.Valid() {
			t.Errorf("%s should be valid", s)
		}
	}
	if IdeaStatus("exploring").Valid() || IdeaStatus("").Valid() {
		t.Error("statuses are case-sensitive and non-empty")
	}
	open := map[IdeaStatus]bool{StatusExploring: true, StatusActive: true, StatusDecided: true, StatusReadyToImplement: true}
	for _, s := range AllIdeaStatuses {
		if s.IsOpen() != open[s] {
			t.Errorf("%s.IsOpen() = %v", s, s.IsOpen())
		}
	}
}

func TestStatusForOutcome(t *testing.T) {
	want := map[Outcome]IdeaStatus{
		OutcomeImplement: StatusReadyToImplement, OutcomeDecision: StatusDecided, OutcomePark: StatusParked,
		OutcomeAbandon: StatusAbandoned, OutcomeMerge: StatusMerged, OutcomeActionPlan: StatusConcluded,
		OutcomeResearchComplete: StatusConcluded, OutcomeProposal: StatusConcluded, OutcomeReference: StatusConcluded, OutcomeOther: StatusConcluded,
	}
	for _, o := range AllOutcomes {
		if !o.Valid() {
			t.Errorf("%s should be valid", o)
		}
		if got := StatusForOutcome(o); got != want[o] {
			t.Errorf("StatusForOutcome(%s) = %s, want %s", o, got, want[o])
		}
	}
	if Outcome("SHIP").Valid() {
		t.Error("unknown outcome must be invalid")
	}
}

func TestParseKnowledgeKind(t *testing.T) {
	tests := []struct {
		in   string
		want KnowledgeKind
		ok   bool
	}{
		{"decision", KindDecision, true},
		{"Decisions", KindDecision, true},
		{"  assumption ", KindAssumption, true},
		{"ASSUMPTIONS", KindAssumption, true},
		{"evidence", KindEvidence, true},
		{"evidences", KindEvidence, true},
		{"insight", KindInsight, true},
		{"learnings", KindInsight, true},
		{"lesson", KindInsight, true},
		{"question", KindQuestion, true},
		{"open_questions", KindQuestion, true},
		{"action", KindAction, true},
		{"action_items", KindAction, true},
		{"tasks", KindAction, true},
		{"next_step", KindAction, true},
		{"", "", false},
		{"idea", "", false},
		{"decisionz", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseKnowledgeKind(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseKnowledgeKind(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestKnowledgeKindMetadata(t *testing.T) {
	prefixes := map[KnowledgeKind]string{KindDecision: "D", KindAssumption: "A", KindEvidence: "E", KindInsight: "I", KindQuestion: "Q", KindAction: "T"}
	defaults := map[KnowledgeKind]string{KindDecision: "ACTIVE", KindAssumption: "UNVALIDATED", KindEvidence: "ACTIVE", KindInsight: "ACTIVE", KindQuestion: "OPEN", KindAction: "TODO"}
	for _, k := range AllKnowledgeKinds {
		if !k.Valid() {
			t.Errorf("%s should be valid", k)
		}
		if k.RefPrefix() != prefixes[k] {
			t.Errorf("%s prefix = %s", k, k.RefPrefix())
		}
		if k.Label(7) != prefixes[k]+"7" {
			t.Errorf("%s label = %s", k, k.Label(7))
		}
		if k.DefaultStatus() != defaults[k] || !k.ValidStatus(k.DefaultStatus()) {
			t.Errorf("%s default status %s invalid", k, k.DefaultStatus())
		}
		if k.ValidStatus("NOPE") {
			t.Errorf("%s accepts bogus status", k)
		}
		if k.EntityType() != EntityType(k) || !k.EntityType().IsKnowledge() {
			t.Errorf("%s entity type mapping broken", k)
		}
	}
	if KnowledgeKind("Decision").Valid() {
		t.Error("Valid must require the canonical lowercase form")
	}
	if KindDecision.ValidStatus("UNVALIDATED") || !KindAssumption.ValidStatus("INVALIDATED") {
		t.Error("statuses must be kind-specific")
	}
}

func TestIsLiveStatus(t *testing.T) {
	for _, s := range []string{"SUPERSEDED", "REVERSED", "REJECTED", "RETRACTED", "INVALIDATED", "DROPPED"} {
		if IsLiveStatus(s) {
			t.Errorf("%s must not be live", s)
		}
	}
	for _, s := range []string{"ACTIVE", "UNVALIDATED", "VALIDATED", "OPEN", "ANSWERED", "TODO", "DONE", "DISPUTED", "PROPOSED"} {
		if !IsLiveStatus(s) {
			t.Errorf("%s must be live", s)
		}
	}
	it := KnowledgeItem{Kind: KindDecision, Status: "ACTIVE", ReviewState: ReviewProposed}
	if it.IsLive() {
		t.Error("a PROPOSED item is not durable knowledge")
	}
	it.ReviewState = ReviewAccepted
	if !it.IsLive() {
		t.Error("an accepted ACTIVE decision is live")
	}
}

func TestParseRefLabel(t *testing.T) {
	tests := []struct {
		in   string
		kind KnowledgeKind
		n    int
		ok   bool
	}{
		{"D3", KindDecision, 3, true},
		{"d12", KindDecision, 12, true},
		{" A1 ", KindAssumption, 1, true},
		{"E2", KindEvidence, 2, true},
		{"I4", KindInsight, 4, true},
		{"Q9", KindQuestion, 9, true},
		{"T2", KindAction, 2, true},
		{"CP4", "", 0, false}, // checkpoints are not knowledge labels
		{"D0", "", 0, false},
		{"D-1", "", 0, false},
		{"X3", "", 0, false},
		{"D", "", 0, false},
		{"", "", 0, false},
		{"Dx", "", 0, false},
	}
	for _, tt := range tests {
		k, n, ok := ParseRefLabel(tt.in)
		if k != tt.kind || n != tt.n || ok != tt.ok {
			t.Errorf("ParseRefLabel(%q) = (%q, %d, %v), want (%q, %d, %v)", tt.in, k, n, ok, tt.kind, tt.n, tt.ok)
		}
	}
}

func TestParseArtifactType(t *testing.T) {
	tests := []struct {
		in   string
		want ArtifactType
		ok   bool
	}{
		{"ACTION_PLAN", ArtifactActionPlan, true},
		{"action plan", ArtifactActionPlan, true},
		{"implementation-prompt", ArtifactImplementationPrompt, true},
		{"prompt", ArtifactImplementationPrompt, true},
		{"coding prompt", ArtifactImplementationPrompt, true},
		{"spec", ArtifactTechnicalSpec, true},
		{"Technical Specification", ArtifactTechnicalSpec, true},
		{"prd", ArtifactRequirements, true},
		{"roadmap", ArtifactActionPlan, true},
		{"memo", ArtifactDecisionMemo, true},
		{"markdown", ArtifactCustom, true},
		{"  executive_summary ", ArtifactExecutiveSummary, true},
		{"poem", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseArtifactType(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseArtifactType(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
	for _, at := range AllArtifactTypes {
		if !at.Valid() {
			t.Errorf("%s should be valid", at)
		}
		if got, ok := ParseArtifactType(string(at)); !ok || got != at {
			t.Errorf("round trip of %s failed", at)
		}
	}
	if ArtifactImplementationPrompt.HumanName() != "Implementation Prompt" || ArtifactActionPlan.HumanName() != "Action Plan" {
		t.Errorf("HumanName: %q / %q", ArtifactImplementationPrompt.HumanName(), ArtifactActionPlan.HumanName())
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Biker Community Platform", "biker-community-platform"},
		{"  Hello,   World!  ", "hello-world"},
		{"Zoho → SharePoint (v2)", "zoho-sharepoint-v2"},
		{"---", "untitled"},
		{"", "untitled"},
		{"Ünïcödé only", "n-c-d-only"},
		{"AI/ML: 2026 plans", "ai-ml-2026-plans"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	long := Slugify(strings.Repeat("word ", 40))
	if len(long) > 61 || strings.HasSuffix(long, "-") || strings.HasPrefix(long, "-") {
		t.Errorf("long slug not bounded/trimmed: %q (%d)", long, len(long))
	}
}

func TestEnsureAttrsDefaults(t *testing.T) {
	cases := map[KnowledgeKind]func(*KnowledgeItem) bool{
		KindDecision:   func(k *KnowledgeItem) bool { return k.Decision != nil && k.Decision.Alternatives != nil },
		KindAssumption: func(k *KnowledgeItem) bool { return k.Assumption != nil && k.Assumption.Risk == "MEDIUM" },
		KindEvidence: func(k *KnowledgeItem) bool {
			return k.Evidence != nil && k.Evidence.Stance == "SUPPORTS" && k.Evidence.Strength == "MODERATE"
		},
		KindInsight:  func(k *KnowledgeItem) bool { return k.Insight != nil && k.Insight.Importance == "MEDIUM" },
		KindQuestion: func(k *KnowledgeItem) bool { return k.Question != nil },
		KindAction:   func(k *KnowledgeItem) bool { return k.Action != nil && k.Action.Priority == "MEDIUM" },
	}
	for kind, check := range cases {
		it := &KnowledgeItem{Kind: kind}
		it.EnsureAttrs()
		if !check(it) {
			t.Errorf("EnsureAttrs(%s) did not set defaults: %+v", kind, it)
		}
	}
	// Existing attributes are preserved.
	it := &KnowledgeItem{Kind: KindAssumption, Assumption: &AssumptionAttrs{Risk: "HIGH"}}
	it.EnsureAttrs()
	if it.Assumption.Risk != "HIGH" {
		t.Error("EnsureAttrs overwrote existing attributes")
	}
}

func TestErrorsAndRefs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", NotFound("idea"))
	if !IsNotFound(err) || KindOf(err) != KindNotFound {
		t.Errorf("KindOf through wrapping failed: %v", KindOf(err))
	}
	if KindOf(errors.New("plain")) != "" {
		t.Error("plain errors have no kind")
	}
	inv := Invalid("title", "too long")
	if !strings.Contains(inv.Error(), "(title)") || KindOf(inv) != KindInvalid {
		t.Errorf("Invalid error: %v", inv)
	}
	for _, e := range []error{Conflict("x"), Forbidden("x"), Unauthorized("x"), Immutable("x"), RateLimited("x"), Unavailable("x")} {
		if KindOf(e) == "" {
			t.Errorf("%v has no kind", e)
		}
	}
	id := uuid.MustParse("6a1f0c2e-1b2c-4d3e-9f10-aa11bb22cc01")
	if got := (EntityRef{Type: EntityDecision, ID: id}).Link(); got != "iv://decision/"+id.String() {
		t.Errorf("Link() = %s", got)
	}
	for _, et := range AllEntityTypes {
		if !et.Valid() {
			t.Errorf("%s should be valid", et)
		}
	}
	for _, r := range AllRelTypes {
		if !r.Valid() {
			t.Errorf("%s should be valid", r)
		}
	}
	if RelType("loves").Valid() || EntityType("user").Valid() {
		t.Error("unknown rel/entity types must be invalid")
	}
	if !OriginSource.Valid() || !OriginInterpretation.Valid() || Origin("GUESS").Valid() {
		t.Error("origin validation broken")
	}
	if !BranchActive.Valid() || BranchStatus("DELETED").Valid() {
		t.Error("branch status validation broken")
	}
}

func TestSnapshotHelpers(t *testing.T) {
	s := &Snapshot{Items: []KnowledgeItem{{Kind: KindDecision}, {Kind: KindDecision}, {Kind: KindQuestion}}}
	if n := len(s.ItemsOfKind(KindDecision)); n != 2 {
		t.Errorf("ItemsOfKind = %d", n)
	}
	c := s.CountByKind()
	if c["decision"] != 2 || c["question"] != 1 || c["evidence"] != 0 {
		t.Errorf("CountByKind = %v", c)
	}
}
