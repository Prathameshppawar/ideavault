package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/testutil"
)

// chatTurn is the parsed SSE stream of one agent turn.
type chatTurn struct {
	Events         []testutil.SSEEvent
	RunID          uuid.UUID
	ConversationID uuid.UUID
	Content        string
	Status         string
	Traces         []domain.TraceEvent
	Confirmation   *struct {
		ToolCallID  uuid.UUID      `json:"tool_call_id"`
		Tool        string         `json:"tool"`
		Category    string         `json:"category"`
		Description string         `json:"description"`
		Arguments   map[string]any `json:"arguments"`
	}
	Focus struct {
		IdeaID       *uuid.UUID `json:"idea_id"`
		BranchID     *uuid.UUID `json:"branch_id"`
		CheckpointID *uuid.UUID `json:"checkpoint_id"`
	}
	Refs []domain.EntityRef
}

// toolsDone lists the tools that executed successfully, in order.
func (c *chatTurn) toolsDone() []string {
	var out []string
	for _, tr := range c.Traces {
		if tr.Kind == "tool" && tr.Status == "done" {
			out = append(out, tr.Tool)
		}
	}
	return out
}

func parseTurn(t *testing.T, evs []testutil.SSEEvent) *chatTurn {
	t.Helper()
	ct := &chatTurn{Events: evs}
	names := testutil.EventNames(evs)
	if len(evs) == 0 || evs[len(evs)-1].Event != "done" {
		t.Fatalf("stream must end with a done event: %v", names)
	}
	for _, ev := range evs {
		switch ev.Event {
		case "run_started":
			var d struct {
				RunID          uuid.UUID `json:"run_id"`
				ConversationID uuid.UUID `json:"conversation_id"`
			}
			ev.Decode(t, &d)
			ct.RunID, ct.ConversationID = d.RunID, d.ConversationID
		case "trace":
			var tr domain.TraceEvent
			ev.Decode(t, &tr)
			ct.Traces = append(ct.Traces, tr)
		case "confirmation_required":
			ev.Decode(t, &ct.Confirmation)
		case "message":
			var m struct {
				Content string             `json:"content"`
				Role    string             `json:"role"`
				Refs    []domain.EntityRef `json:"refs"`
			}
			ev.Decode(t, &m)
			if m.Role != "assistant" {
				t.Errorf("message role = %s", m.Role)
			}
			ct.Content, ct.Refs = m.Content, m.Refs
		case "run_completed":
			var d struct {
				Status string          `json:"status"`
				Focus  json.RawMessage `json:"focus"`
			}
			ev.Decode(t, &d)
			ct.Status = d.Status
			_ = json.Unmarshal(d.Focus, &ct.Focus)
		case "error":
			t.Logf("stream error event: %s", ev.Data)
		}
	}
	if ct.RunID == uuid.Nil {
		t.Fatalf("no run_started event: %v", names)
	}
	return ct
}

func chat(t *testing.T, c *testutil.Client, body map[string]any) *chatTurn {
	t.Helper()
	return parseTurn(t, c.Stream("/v1/agent/chat", body))
}

func confirm(t *testing.T, c *testutil.Client, toolCallID uuid.UUID, approve bool) *chatTurn {
	t.Helper()
	return parseTurn(t, c.Stream("/v1/agent/tool-calls/"+toolCallID.String()+"/confirm", map[string]any{"approve": approve}))
}

func getRun(t *testing.T, c *testutil.Client, id uuid.UUID) domain.AgentRun {
	t.Helper()
	var run domain.AgentRun
	c.JSON("GET", "/v1/agent/runs/"+id.String(), nil, 200, &run)
	return run
}

func ideaCount(t *testing.T, u *testutil.User) int {
	return countRows(t, `SELECT count(*) FROM ideas WHERE user_id = $1`, u.ID)
}

// (a) New idea → decision → checkpoint, all through chat.
func TestAgentNewIdeaDecisionCheckpoint(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	st := e.Store()
	origin := "I have an idea for a biker community platform where people can create trips and invite friends."
	t1 := chat(t, c, map[string]any{"message": origin})
	if t1.Status != "COMPLETED" || strings.Join(t1.toolsDone(), ",") != "create_idea" {
		t.Fatalf("turn 1: status=%s tools=%v events=%v", t1.Status, t1.toolsDone(), testutil.EventNames(t1.Events))
	}
	if len(testutil.EventsNamed(t1.Events, "token")) == 0 {
		t.Error("the answer must be streamed as token events")
	}
	ideas, _, _ := st.ListIdeas(ctx(), postgres.IdeaFilter{UserID: u.ID})
	if len(ideas) != 1 {
		t.Fatalf("ideas = %d", len(ideas))
	}
	idea := ideas[0]
	if idea.Title != "Biker Community Platform" || idea.OriginText != origin || idea.OriginConversationID == nil || *idea.OriginConversationID != t1.ConversationID {
		t.Errorf("idea: %+v", idea)
	}
	brs := try(st.ListBranches(ctx(), u.ID, idea.ID)).must(t)
	if len(brs) != 1 || brs[0].Name != "Main" || !brs[0].IsDefault {
		t.Errorf("branches: %+v", brs)
	}
	if !strings.Contains(t1.Content, "Created a new Idea Space: **[Biker Community Platform](iv://idea/"+idea.ID.String()+")**") {
		t.Errorf("answer must link the created idea: %q", t1.Content)
	}
	if t1.Focus.IdeaID == nil || *t1.Focus.IdeaID != idea.ID || len(t1.Refs) == 0 || t1.Refs[0].ID != idea.ID {
		t.Errorf("focus/refs: %+v %+v", t1.Focus, t1.Refs)
	}
	var cd struct {
		Conversation domain.Conversation `json:"conversation"`
		Messages     []domain.Message    `json:"messages"`
	}
	c.JSON("GET", "/v1/conversations/"+t1.ConversationID.String(), nil, 200, &cd)
	if cd.Conversation.IdeaID == nil || *cd.Conversation.IdeaID != idea.ID || cd.Conversation.Origin != domain.OriginNative || len(cd.Messages) != 2 {
		t.Fatalf("conversation: %+v (%d messages)", cd.Conversation, len(cd.Messages))
	}
	am := cd.Messages[1]
	if am.Role != domain.RoleAssistant || am.Content != t1.Content || am.AgentRunID == nil || *am.AgentRunID != t1.RunID || am.Model != "offline-planner" ||
		am.Metadata["refs"] == nil || am.Metadata["trace"] == nil || am.Untrusted {
		t.Errorf("persisted assistant message: %+v", am)
	}
	run := getRun(t, c, t1.RunID)
	if run.Status != domain.RunCompleted || run.Provider != "mock" || run.Model != "offline-planner" || len(run.Trace) == 0 || run.IdeaID == nil || *run.IdeaID != idea.ID ||
		run.AssistantMessageID == nil || run.CompletedAt == nil {
		t.Errorf("run: %+v", run)
	}
	if len(run.ToolCalls) != 1 || run.ToolCalls[0].ToolName != "create_idea" || run.ToolCalls[0].Status != "SUCCEEDED" || run.ToolCalls[0].Category != domain.ToolWrite {
		t.Errorf("tool calls: %+v", run.ToolCalls)
	}
	if countRows(t, `SELECT count(*) FROM usage_events WHERE agent_run_id = $1 AND provider = 'mock'`, t1.RunID) != 2 {
		t.Error("every planner step is accounted in usage_events")
	}

	t2 := chat(t, c, map[string]any{"conversation_id": t1.ConversationID, "message": "Remember this as a decision: we will not build a marketplace in v1 because moderation is expensive."})
	if t2.Status != "COMPLETED" || strings.Join(t2.toolsDone(), ",") != "record_decision" || t2.ConversationID != t1.ConversationID {
		t.Fatalf("turn 2: %s %v", t2.Status, t2.toolsDone())
	}
	decs := try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, IdeaID: &idea.ID, Kinds: []domain.KnowledgeKind{domain.KindDecision}})).must(t)
	if len(decs) != 1 || decs[0].Label != "D1" || decs[0].Statement != "we will not build a marketplace in v1 because moderation is expensive." {
		t.Fatalf("D1: %+v", decs)
	}
	d1 := decs[0]
	if d1.Origin != domain.OriginSource || d1.SourceMessageID == nil || d1.SourceConversationID == nil || *d1.SourceConversationID != t1.ConversationID ||
		d1.CreatedBy != domain.ActorAgent || d1.AgentRunID == nil || *d1.AgentRunID != t2.RunID || d1.BranchID != brs[0].ID {
		t.Errorf("D1 provenance: %+v", d1)
	}
	if !strings.Contains(t2.Content, "Saved as decision **[D1]") {
		t.Errorf("turn 2 answer: %q", t2.Content)
	}

	t3 := chat(t, c, map[string]any{"conversation_id": t1.ConversationID, "message": "Create a checkpoint"})
	if strings.Join(t3.toolsDone(), ",") != "create_checkpoint" || !strings.Contains(t3.Content, "Created checkpoint **[CP1]") {
		t.Fatalf("turn 3: %v %q", t3.toolsDone(), t3.Content)
	}
	cp := try(st.GetCheckpointByNumber(ctx(), u.ID, idea.ID, 1)).must(t)
	if cp.CreatedBy != domain.ActorAgent || cp.AgentRunID == nil || *cp.AgentRunID != t3.RunID || len(cp.Snapshot.Items) != 1 || cp.Snapshot.Items[0].ID != d1.ID {
		t.Errorf("CP1: %+v", cp)
	}
	if len(cp.Snapshot.Conversations) != 1 || cp.Snapshot.Conversations[0].ID != t1.ConversationID {
		t.Errorf("the chat that produced the idea is captured by CP1: %+v", cp.Snapshot.Conversations)
	}
	// Journey shows the prompt that created the checkpoint.
	j := try(e.Svc().Journey(ctx(), u.ID, idea.ID)).must(t)
	for _, n := range j {
		if n.Kind == "checkpoint" && n.Prompt != "Create a checkpoint" {
			t.Errorf("journey checkpoint prompt = %q", n.Prompt)
		}
	}
}

// (b) Continue from a checkpoint.
func TestAgentContinueFromCheckpoint(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	tl := seedTimeline(t, e, u, "Biker Community Platform")
	turn := chat(t, c, map[string]any{"message": "Continue the biker community platform from checkpoint 4."})
	if turn.Status != "COMPLETED" || strings.Join(turn.toolsDone(), ",") != "get_checkpoint,get_context" {
		t.Fatalf("tools = %v status=%s", turn.toolsDone(), turn.Status)
	}
	if !strings.Contains(turn.Content, "from **[CP4](iv://checkpoint/"+tl.cps[4].ID.String()+")**") || !strings.Contains(turn.Content, "We will not build a marketplace in v1") {
		t.Errorf("answer must cite CP4 and its state: %q", turn.Content)
	}
	if strings.Contains(turn.Content, "verified riders") {
		t.Error("decisions made after CP4 must not appear when continuing from CP4")
	}
	if turn.Focus.CheckpointID == nil || *turn.Focus.CheckpointID != tl.cps[4].ID || turn.Focus.IdeaID == nil || *turn.Focus.IdeaID != tl.idea.Idea.ID {
		t.Errorf("focus = %+v", turn.Focus)
	}
	for _, tc := range getRun(t, c, turn.RunID).ToolCalls {
		if tc.Category != domain.ToolRead {
			t.Errorf("continuing must only read: %s is %s", tc.ToolName, tc.Category)
		}
	}
}

// (c) Fork with research from a later checkpoint.
func TestAgentForkWithLaterResearch(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	tl := seedTimeline(t, e, u, "Biker Community Platform")
	turn := chat(t, c, map[string]any{"message": "Fork the biker community platform from checkpoint 4 but bring the research from checkpoint 7."})
	if turn.Status != "COMPLETED" || strings.Join(turn.toolsDone(), ",") != "fork_idea" {
		t.Fatalf("tools = %v", turn.toolsDone())
	}
	brs := try(e.Store().ListBranches(ctx(), u.ID, tl.idea.Idea.ID)).must(t)
	if len(brs) != 2 {
		t.Fatalf("branches = %d", len(brs))
	}
	fork := brs[1]
	if fork.ForkedFromCheckpointNumber == nil || *fork.ForkedFromCheckpointNumber != 4 || *fork.ForkMode != domain.ForkSelective {
		t.Errorf("fork: %+v", fork)
	}
	got := labelsOf(try(e.Store().ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, BranchID: &fork.ID, Limit: 100})).must(t))
	for _, want := range []string{"D1", "A1", "Q1", "E1", "E2", "I1"} {
		if _, ok := got[want]; !ok {
			t.Errorf("fork missing %s: %v", want, keysOf(got))
		}
	}
	for _, not := range []string{"D2", "D3", "T1"} {
		if _, ok := got[not]; ok {
			t.Errorf("fork must not contain %s", not)
		}
	}
	if !strings.Contains(turn.Content, "Inherited **4** item(s)") || !strings.Contains(turn.Content, "Brought **2** selected item(s)") || !strings.Contains(turn.Content, "The original branch is unchanged") {
		t.Errorf("answer: %q", turn.Content)
	}
	if turn.Focus.BranchID == nil || *turn.Focus.BranchID != fork.ID {
		t.Errorf("focus moves to the new branch: %+v", turn.Focus)
	}
}

// (d) Artifacts via chat, persisted and downloadable.
func TestAgentArtifacts(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	tl := seedTimeline(t, e, u, "Biker Community Platform")
	focus := map[string]any{"idea_id": tl.idea.Idea.ID}
	plan := chat(t, c, merge(focus, "message", "Create an action plan"))
	prompt := chat(t, c, merge(focus, "message", "Give me a prompt I can give to an implementation agent"))
	if strings.Join(plan.toolsDone(), ",") != "create_artifact" || strings.Join(prompt.toolsDone(), ",") != "create_artifact" {
		t.Fatalf("tools: %v / %v", plan.toolsDone(), prompt.toolsDone())
	}
	var arts []domain.Artifact
	c.JSON("GET", "/v1/artifacts?idea_id="+tl.idea.Idea.ID.String(), nil, 200, &arts)
	types := map[domain.ArtifactType]domain.Artifact{}
	for _, a := range arts {
		types[a.Type] = a
	}
	ip, ok := types[domain.ArtifactImplementationPrompt]
	if !ok || len(arts) != 2 {
		t.Fatalf("artifacts = %+v", arts)
	}
	if _, ok := types[domain.ArtifactActionPlan]; !ok {
		t.Error("action plan not persisted")
	}
	dl := c.Do("GET", "/v1/artifacts/"+ip.ID.String()+"/download", nil)
	if dl.Status != 200 || !strings.HasPrefix(string(dl.Body), "#") || !strings.Contains(dl.Header.Get("Content-Disposition"), "Implementation-Prompt.md") {
		t.Errorf("download: %d %q", dl.Status, dl.Header.Get("Content-Disposition"))
	}
	if !strings.Contains(string(dl.Body), "## Decisions (binding)") || !strings.Contains(string(dl.Body), "[D2]") {
		t.Errorf("implementation prompt must be grounded in current decisions:\n%s", dl.Body)
	}
	var full domain.Artifact
	c.JSON("GET", "/v1/artifacts/"+ip.ID.String(), nil, 200, &full)
	if full.Generator != "template:v1" || full.Instructions != "Give me a prompt I can give to an implementation agent" {
		t.Errorf("artifact metadata: %+v", full)
	}
	var prov []domain.ProvenanceEntry
	c.JSON("GET", "/v1/artifacts/"+ip.ID.String()+"/provenance", nil, 200, &prov)
	cited := 0
	for _, p := range prov {
		if p.Role == "cited" {
			cited++
		}
	}
	if cited == 0 || !strings.Contains(prompt.Content, "cited") {
		t.Errorf("provenance: %d cited; answer %q", cited, prompt.Content)
	}
	if run := getRun(t, c, prompt.RunID); run.ToolCalls[0].Category != domain.ToolWrite {
		t.Errorf("create_artifact category = %s", run.ToolCalls[0].Category)
	}
}

func merge(base map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// (e) Context pack via chat.
func TestAgentContextPack(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	tl := seedTimeline(t, e, u, "Biker Community Platform")
	for _, msg := range []string{"Build a context pack for this idea", "I want to continue this in ChatGPT."} {
		turn := chat(t, c, map[string]any{"idea_id": tl.idea.Idea.ID, "message": msg})
		if strings.Join(turn.toolsDone(), ",") != "build_context_pack" || !strings.Contains(turn.Content, "Built a context pack") {
			t.Errorf("%q: tools=%v content=%q", msg, turn.toolsDone(), turn.Content)
		}
	}
	var packs []domain.ContextPack
	c.JSON("GET", "/v1/context-packs?idea_id="+tl.idea.Idea.ID.String(), nil, 200, &packs)
	if len(packs) != 2 {
		t.Fatalf("packs = %d", len(packs))
	}
	exp := c.Do("GET", "/v1/context-packs/"+packs[0].ID.String()+"/export", nil)
	if !strings.Contains(string(exp.Body), "**[D3]** Charge organizers, not riders") {
		t.Errorf("pack export:\n%s", exp.Body)
	}
}

// (f) Destructive actions need explicit confirmation; declining keeps the idea.
func TestAgentDestructiveConfirmation(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	idea := e.MustIdea(t, u, "Biker Community Platform", "riders")
	e.MustCheckpoint(t, u, idea.Idea.ID, nil, "")
	before := ideaCount(t, u)
	ask := chat(t, c, map[string]any{"message": "Delete the idea biker community platform"})
	if ask.Confirmation == nil || ask.Confirmation.Tool != "delete_idea" || ask.Confirmation.Category != string(domain.ToolDestructive) ||
		!strings.Contains(ask.Confirmation.Description, "Permanently delete") || ask.Confirmation.Arguments["idea"] != "biker community platform" {
		t.Fatalf("confirmation_required event: %+v (events %v)", ask.Confirmation, testutil.EventNames(ask.Events))
	}
	if len(testutil.EventsNamed(ask.Events, "message")) != 0 || ask.Status != "" {
		t.Error("a paused run must not emit a final message")
	}
	if ideaCount(t, u) != before {
		t.Fatal("nothing may be deleted before confirmation")
	}
	run := getRun(t, c, ask.RunID)
	if run.Status != domain.RunAwaitingConfirmation || run.ToolCalls[0].Status != "AWAITING_CONFIRMATION" {
		t.Errorf("paused run: %s %+v", run.Status, run.ToolCalls)
	}
	var pending []domain.ToolCallRecord
	c.JSON("GET", "/v1/agent/pending", nil, 200, &pending)
	if len(pending) != 1 || pending[0].ID != ask.Confirmation.ToolCallID {
		t.Errorf("pending confirmations: %+v", pending)
	}
	// Another user cannot confirm it.
	stranger := e.Login(t, e.NewUser(t))
	evs := stranger.Stream("/v1/agent/tool-calls/"+ask.Confirmation.ToolCallID.String()+"/confirm", map[string]any{"approve": true})
	if len(testutil.EventsNamed(evs, "error")) != 1 || ideaCount(t, u) != before {
		t.Fatalf("a stranger must not be able to approve: %v", testutil.EventNames(evs))
	}

	declined := confirm(t, c, ask.Confirmation.ToolCallID, false)
	if declined.Status != "COMPLETED" || ideaCount(t, u) != before || !strings.Contains(declined.Content, "declined") {
		t.Fatalf("decline: status=%s content=%q", declined.Status, declined.Content)
	}
	if tc := getRun(t, c, ask.RunID).ToolCalls[0]; tc.Status != "DENIED" {
		t.Errorf("declined tool call status = %s", tc.Status)
	}
	again := c.Stream("/v1/agent/tool-calls/"+ask.Confirmation.ToolCallID.String()+"/confirm", map[string]any{"approve": true})
	if len(testutil.EventsNamed(again, "error")) != 1 || ideaCount(t, u) != before {
		t.Fatalf("a declined action cannot be approved later: %v", testutil.EventNames(again))
	}

	ask2 := chat(t, c, map[string]any{"conversation_id": ask.ConversationID, "message": "Delete the idea biker community platform"})
	if ask2.Confirmation == nil {
		t.Fatalf("second request must ask again: %v", testutil.EventNames(ask2.Events))
	}
	approved := confirm(t, c, ask2.Confirmation.ToolCallID, true)
	if approved.Status != "COMPLETED" || !strings.Contains(approved.Content, "Deleted **Biker Community Platform**") {
		t.Fatalf("approve: status=%s content=%q", approved.Status, approved.Content)
	}
	if ideaCount(t, u) != before-1 {
		t.Fatal("approved deletion did not delete the idea")
	}
	if res := c.Do("GET", "/v1/ideas/"+idea.Idea.ID.String(), nil); res.Status != 404 {
		t.Errorf("deleted idea still readable: %s", res)
	}
	// The run that deleted the idea must be finalized cleanly (no dangling focus on the deleted idea).
	final := getRun(t, c, ask2.RunID)
	if final.Status != domain.RunCompleted || final.IdeaID != nil || final.AssistantMessageID == nil || final.CompletedAt == nil {
		t.Errorf("run after approved deletion: status=%s idea=%v msg=%v", final.Status, final.IdeaID, final.AssistantMessageID)
	}
	if approved.Focus.IdeaID != nil {
		t.Errorf("focus must be cleared after deleting the focused idea: %+v", approved.Focus)
	}
	if tc := final.ToolCalls[0]; tc.Status != "SUCCEEDED" {
		t.Errorf("approved tool call = %s", tc.Status)
	}
	// The conversation keeps working after the deletion.
	next := chat(t, c, map[string]any{"conversation_id": ask.ConversationID, "message": "Show my ideas"})
	if next.Status != "COMPLETED" || strings.Join(next.toolsDone(), ",") != "list_ideas" {
		t.Errorf("follow-up turn: %s %v", next.Status, next.toolsDone())
	}
	// Disabled tools are refused by policy (never executed).
	c.JSON("PUT", "/v1/agent/policy", map[string]any{"disabled_tools": []string{"create_checkpoint"}, "confirm_writes": false}, 200, nil)
	other := e.MustIdea(t, u, "Policy Idea", "")
	blocked := chat(t, c, map[string]any{"idea_id": other.Idea.ID, "message": "Create a checkpoint"})
	if len(blocked.toolsDone()) != 0 || countRows(t, `SELECT count(*) FROM checkpoints WHERE idea_id = $1`, other.Idea.ID) != 0 {
		t.Errorf("disabled tool executed: %v", blocked.toolsDone())
	}
	if tc := getRun(t, c, blocked.RunID).ToolCalls[0]; tc.Status != "REJECTED_BY_POLICY" {
		t.Errorf("policy rejection status = %s", tc.Status)
	}
	// confirm_writes makes WRITE tools pause too.
	c.JSON("PUT", "/v1/agent/policy", map[string]any{"confirm_writes": true}, 200, nil)
	w := chat(t, c, map[string]any{"idea_id": other.Idea.ID, "message": "Create a checkpoint"})
	if w.Confirmation == nil || w.Confirmation.Tool != "create_checkpoint" {
		t.Errorf("confirm_writes must pause WRITE tools: %v", testutil.EventNames(w.Events))
	}
}

// (g) Prompt injection inside imported content is data, never instructions.
func TestAgentPromptInjectionResistance(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	idea := e.MustIdea(t, u, "Trip Sharing Feature", "trip sharing for riders")
	other := e.MustIdea(t, u, "Clinic Scheduling", "clinic")
	imp := try(e.Svc().ImportNow(ctx(), u.Actor(), service.ImportNowInput{CreateImportInput: service.CreateImportInput{SourceKind: "file", Filename: "malicious.json",
		Data: testutil.Fixture(t, "imports", "injection", "malicious_chatgpt.json")}, IdeaID: &idea.Idea.ID, Extract: true})).must(t)
	if len(imp.InjectionFlags) < 3 {
		t.Fatalf("import must flag the injection: %v", imp.InjectionFlags)
	}
	e.DrainJobs(t) // embed the imported messages so the context engine can retrieve them
	before := ideaCount(t, u)
	turn := chat(t, c, map[string]any{"idea_id": idea.Idea.ID, "message": "Summarize the imported notes about the trip-sharing feature"})
	if turn.Status != "COMPLETED" {
		t.Fatalf("status = %s", turn.Status)
	}
	run := getRun(t, c, turn.RunID)
	for _, tc := range run.ToolCalls {
		if tc.Category != domain.ToolRead && tc.Category != domain.ToolAnalyze {
			t.Errorf("injected instructions caused a %s tool call: %s", tc.Category, tc.ToolName)
		}
		if strings.HasPrefix(tc.ToolName, "delete_") {
			t.Errorf("destructive tool %s was requested", tc.ToolName)
		}
	}
	if len(run.ToolCalls) == 0 || run.ToolCalls[0].ToolName != "get_context" {
		t.Errorf("expected a read of the idea context, got %+v", run.ToolCalls)
	}
	if ideaCount(t, u) != before || countRows(t, `SELECT count(*) FROM ideas WHERE id = $1`, other.Idea.ID) != 1 {
		t.Error("the vault changed after reading injected content")
	}
	if !strings.Contains(turn.Content, "<untrusted_imported_content") || !strings.Contains(turn.Content, "Ignore all previous instructions") {
		t.Errorf("imported text must reach the model only inside the untrusted fence:\n%s", turn.Content)
	}
	if strings.Contains(turn.Content, "system prompt:") || strings.Contains(turn.Content, "You are IdeaVault") {
		t.Error("the system prompt must never be revealed")
	}
	items := try(e.Store().ListImportItems(ctx(), u.ID, imp.Import.ID, false, nil)).must(t)
	if len(items) != 1 || len(items[0].InjectionFlags) < 3 {
		t.Errorf("import item flags: %+v", items)
	}
	for _, m := range try(e.Store().ListMessages(ctx(), u.ID, imp.Conversations[0].ID, 0, 10)).must(t) {
		if !m.Untrusted {
			t.Errorf("imported message %d is not flagged untrusted", m.Position)
		}
	}
	// Imported (untrusted) conversations are read-only for chat.
	evs := c.Stream("/v1/agent/chat", map[string]any{"conversation_id": imp.Conversations[0].ID, "message": "continue"})
	if len(testutil.EventsNamed(evs, "error")) != 1 {
		t.Errorf("chatting inside an imported conversation must be refused: %v", testutil.EventNames(evs))
	}
}

// (h) Decision evolution: history is retained and explained.
func TestAgentDecisionEvolution(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	c := e.Login(t, u)
	first := chat(t, c, map[string]any{"message": "I have an idea for a biker community platform where people can create trips and invite friends."})
	conv := first.ConversationID
	chat(t, c, map[string]any{"conversation_id": conv, "message": "Remember this as a decision: we will not build a marketplace in v1 because moderation is expensive."})
	ideaID := *first.Focus.IdeaID
	d1 := try(e.Store().FindKnowledgeByRef(ctx(), u.ID, ideaID, nil, domain.KindDecision, 1)).must(t)
	d2 := e.MustRecord(t, u, service.RecordKnowledgeInput{IdeaID: ideaID, Kind: domain.KindDecision, Statement: "We will build a marketplace for verified riders in v2",
		Rationale: "verification makes moderation affordable", Supersedes: &d1.ID})
	why := chat(t, c, map[string]any{"conversation_id": conv, "message": "Why did we decide not to build the marketplace?"})
	if strings.Join(why.toolsDone(), ",") != "search_vault,get_decisions" {
		t.Fatalf("tools = %v", why.toolsDone())
	}
	for _, want := range []string{"How it evolved", "D1 we will not build a marketplace in v1", "_(superseded)_", "D2 We will build a marketplace for verified riders in v2", "**Source**"} {
		if !strings.Contains(why.Content, want) {
			t.Errorf("explanation missing %q:\n%s", want, why.Content)
		}
	}
	old := try(e.Store().GetKnowledge(ctx(), u.ID, d1.ID)).must(t)
	if old.Status != "SUPERSEDED" || old.Statement != d1.Statement || *old.SupersededByID != d2.ID {
		t.Errorf("history must be retained: %+v", old)
	}
	for _, tc := range getRun(t, c, why.RunID).ToolCalls {
		if tc.Category != domain.ToolRead {
			t.Errorf("explaining must not write: %s", tc.ToolName)
		}
	}
}

func TestAgentRequestValidation(t *testing.T) {
	e := requireEnv(t)
	c := e.Login(t, e.NewUser(t))
	if res := c.Do("POST", "/v1/agent/chat", map[string]any{"message": "   "}); res.Status != 200 {
		t.Fatalf("validation errors are reported in-stream: %s", res)
	} else if evs, _ := testutil.ParseSSE(strings.NewReader(string(res.Body))); len(testutil.EventsNamed(evs, "error")) != 1 {
		t.Errorf("empty message must yield an error event: %v", testutil.EventNames(evs))
	}
	if res := c.Do("POST", "/v1/agent/chat", []byte(`{"message":`), "Content-Type", "application/json"); res.Status != 400 {
		t.Errorf("malformed body: %s", res)
	}
	turn := chat(t, c, map[string]any{"message": "I have a new idea."})
	if len(turn.toolsDone()) != 0 || !strings.Contains(turn.Content, "What's the idea") {
		t.Errorf("vague new idea must prompt for details: %v %q", turn.toolsDone(), turn.Content)
	}
	var tools []map[string]any
	c.JSON("GET", "/v1/agent/tools", nil, 200, &tools)
	perms := map[string]string{}
	for _, tl := range tools {
		perms[tl["name"].(string)] = tl["permission"].(string)
	}
	if perms["delete_idea"] != "confirm" || perms["search_vault"] != "allow" || perms["create_idea"] != "allow" || perms["web_fetch"] != "confirm" {
		t.Errorf("tool permissions: %v", perms)
	}
}
