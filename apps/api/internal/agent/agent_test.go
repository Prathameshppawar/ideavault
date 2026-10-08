package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/connectors"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/models"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

func TestPolicyDecideMatrix(t *testing.T) {
	cats := []domain.ToolCategory{domain.ToolRead, domain.ToolAnalyze, domain.ToolWrite, domain.ToolDestructive, domain.ToolExternal, "UNKNOWN"}
	tests := []struct {
		name   string
		policy Policy
		want   []Decision // per category, in cats order
	}{
		{"default", Policy{}, []Decision{Allow, Allow, Allow, Confirm, Confirm, Deny}},
		{"confirm writes", Policy{ConfirmWrites: true}, []Decision{Allow, Allow, Confirm, Confirm, Confirm, Deny}},
		{"auto external", Policy{AutoExternal: true}, []Decision{Allow, Allow, Allow, Confirm, Allow, Deny}},
		// DESTRUCTIVE always requires confirmation, whatever else is relaxed.
		{"everything relaxed", Policy{AutoExternal: true}, []Decision{Allow, Allow, Allow, Confirm, Allow, Deny}},
	}
	for _, tt := range tests {
		for i, c := range cats {
			if got := tt.policy.Decide("some_tool", c); got != tt.want[i] {
				t.Errorf("%s: Decide(%s) = %s, want %s", tt.name, c, got, tt.want[i])
			}
		}
	}
	p := Policy{Disabled: []string{"delete_idea", "create_checkpoint", "search_vault"}}
	for _, c := range []struct {
		tool string
		cat  domain.ToolCategory
	}{{"delete_idea", domain.ToolDestructive}, {"create_checkpoint", domain.ToolWrite}, {"search_vault", domain.ToolRead}} {
		if got := p.Decide(c.tool, c.cat); got != Deny {
			t.Errorf("disabled %s must be denied, got %s", c.tool, got)
		}
	}
	if p.Decide("get_idea", domain.ToolRead) != Allow {
		t.Error("only the disabled tools are denied")
	}
}

func TestDescribeCall(t *testing.T) {
	tests := []struct {
		tool Tool
		args map[string]any
		want string
	}{
		{Tool{Name: "delete_idea", Category: domain.ToolDestructive}, map[string]any{"idea": "Biker Platform"}, `Permanently delete the idea "Biker Platform"`},
		{Tool{Name: "delete_artifact", Category: domain.ToolDestructive}, nil, "cannot be undone"},
		{Tool{Name: "web_fetch", Category: domain.ToolExternal}, map[string]any{"url": "https://example.com"}, "https://example.com. Its content will be treated as untrusted"},
		{Tool{Name: "github_create_issue", Category: domain.ToolExternal}, map[string]any{"repo": "me/x", "title": "Bug"}, `Create a GitHub issue in me/x titled "Bug"`},
		{Tool{Name: "other_tool", Category: domain.ToolWrite}, nil, "Run other_tool (write)."},
	}
	for _, tt := range tests {
		if got := describeCall(tt.tool, tt.args); !strings.Contains(got, tt.want) {
			t.Errorf("describeCall(%s) = %q, want it to contain %q", tt.tool.Name, got, tt.want)
		}
	}
}

// planStep runs the offline planner on a conversation and returns its response.
func planStep(t *testing.T, system string, msgs ...models.Message) *models.Response {
	t.Helper()
	res, err := NewOfflinePlanner().Complete(context.Background(), models.Request{System: system, Messages: msgs})
	if err != nil {
		t.Fatalf("planner: %v", err)
	}
	if res.Model != models.MockModel || !res.Usage.Estimated {
		t.Errorf("planner must identify itself and estimate usage: %+v", res)
	}
	return res
}

func user(s string) models.Message { return models.Message{Role: models.RoleUser, Content: s} }

func toolResult(name string, ok bool, data map[string]any) models.Message {
	b, _ := json.Marshal(map[string]any{"ok": ok, "data": data, "summary": name + " done", "error": map[bool]string{false: "boom"}[ok]})
	return models.Message{Role: models.RoleTool, Name: name, ToolCallID: "call_x", Content: string(b)}
}

func args(t *testing.T, tc models.ToolCall) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal(tc.Arguments, &m); err != nil {
		t.Fatalf("tool args %s: %v", tc.Arguments, err)
	}
	return m
}

func TestOfflinePlannerIntents(t *testing.T) {
	const focus = "CURRENT FOCUS: idea \"Biker Community Platform\""
	tests := []struct {
		name    string
		system  string
		msg     string
		tool    string // "" = answers without tools
		check   func(t *testing.T, a map[string]any)
		content string // expected substring when no tool is called
	}{
		{name: "new idea without details asks", msg: "I have a new idea.", content: "What's the idea"},
		{name: "new idea with details creates it", msg: "I have an idea for a biker community platform where people can create trips.", tool: "create_idea",
			check: func(t *testing.T, a map[string]any) {
				if a["origin_text"] != "I have an idea for a biker community platform where people can create trips." {
					t.Errorf("origin must be the user's verbatim words: %v", a)
				}
			}},
		{name: "continue from checkpoint", msg: "Continue the biker platform from checkpoint 4.", tool: "get_checkpoint",
			check: func(t *testing.T, a map[string]any) {
				if a["checkpoint"] != "CP4" || a["idea"] != "biker platform" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "continue without checkpoint opens idea", msg: "Continue with the clinic idea", tool: "get_idea",
			check: func(t *testing.T, a map[string]any) {
				if a["idea"] != "clinic" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "why did we decide", msg: "Why did we decide not to build the marketplace?", tool: "search_vault",
			check: func(t *testing.T, a map[string]any) {
				if a["query"] != "not to build the marketplace" || !reflect.DeepEqual(a["types"], []any{"decision"}) {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "first mention", msg: "Find where I first talked about this.", tool: "search_vault",
			check: func(t *testing.T, a map[string]any) {
				if a["order"] != "oldest" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "fork with research", msg: "Fork this idea from checkpoint 4 but bring the research from checkpoint 7.", tool: "fork_idea",
			check: func(t *testing.T, a map[string]any) {
				if a["from_checkpoint"] != "CP4" {
					t.Errorf("from_checkpoint = %v", a["from_checkpoint"])
				}
				if _, named := a["idea"]; named {
					t.Errorf("'this idea' must use the focused idea, got %v", a["idea"])
				}
				want := []any{map[string]any{"checkpoint": "CP7", "kinds": []any{"evidence", "insight"}}}
				if !reflect.DeepEqual(a["bring"], want) {
					t.Errorf("bring = %v, want %v", a["bring"], want)
				}
			}},
		{name: "fork named idea", msg: "Fork the biker community platform from checkpoint 2 called Premium riders", tool: "fork_idea",
			check: func(t *testing.T, a map[string]any) {
				if a["idea"] != "biker community platform" || a["from_checkpoint"] != "CP2" || a["name"] != "Premium riders" || a["bring"] != nil {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "conclude into action plan", msg: "I think we're done with this idea. Turn everything into an action plan.", tool: "conclude_idea",
			check: func(t *testing.T, a map[string]any) {
				if a["outcome"] != "ACTION_PLAN" || !reflect.DeepEqual(a["generate_artifacts"], []any{"ACTION_PLAN"}) {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "conclude without outcome asks first", msg: "I think we're done with this idea.", tool: "get_idea"},
		{name: "implementation prompt", msg: "Give me a prompt I can give to an implementation agent.", tool: "create_artifact",
			check: func(t *testing.T, a map[string]any) {
				if a["type"] != "IMPLEMENTATION_PROMPT" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "action plan artifact", msg: "Create an action plan", tool: "create_artifact",
			check: func(t *testing.T, a map[string]any) {
				if a["type"] != "ACTION_PLAN" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "remember decision", msg: "Remember this as a decision: we will not build a marketplace in v1 because moderation is expensive.", tool: "record_decision",
			check: func(t *testing.T, a map[string]any) {
				if a["statement"] != "we will not build a marketplace in v1 because moderation is expensive." || a["origin"] != "SOURCE" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "record assumption", msg: "Record this as an assumption: riders will pay for premium route packs", tool: "record_assumption"},
		{name: "save insight", msg: "Save this insight: organizers drive adoption", tool: "record_insight"},
		{name: "checkpoint", msg: "Create a checkpoint", tool: "create_checkpoint"},
		{name: "named checkpoint", msg: "Create a checkpoint called Pricing settled", tool: "create_checkpoint",
			check: func(t *testing.T, a map[string]any) {
				if a["title"] != "Pricing settled" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "delete", msg: "Delete the idea biker community platform", tool: "delete_idea",
			check: func(t *testing.T, a map[string]any) {
				if a["idea"] != "biker community platform" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "context pack", msg: "Build a context pack for this idea", tool: "build_context_pack"},
		// "continue this in ChatGPT" is a context-pack request, not "continue from a checkpoint".
		{name: "continue in chatgpt", msg: "Build a context pack so I can continue this in ChatGPT", tool: "build_context_pack"},
		{name: "continue in claude", msg: "I want to continue this in Claude.", tool: "build_context_pack"},
		{name: "prompt analysis", msg: `Analyze my prompt "write me a plan for the biker app with steps"`, tool: "analyze_prompt",
			check: func(t *testing.T, a map[string]any) {
				if a["text"] != "write me a plan for the biker app with steps" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "thinking patterns", msg: "What are my thinking patterns?", tool: "find_thinking_patterns"},
		{name: "contradictions", msg: "Do any of my decisions contradict each other?", tool: "detect_contradictions"},
		{name: "compare branches", msg: "Compare Main with Premium branch", tool: "compare_branches",
			check: func(t *testing.T, a map[string]any) {
				if a["branch_a"] != "Main" || a["branch_b"] != "Premium" {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "evolution", msg: "How did this idea evolve?", tool: "analyze_idea_evolution"},
		{name: "list ideas", msg: "Show my ideas", tool: "list_ideas"},
		{name: "import share link", msg: "Import this conversation https://chatgpt.com/share/abc-123", tool: "import_conversation",
			check: func(t *testing.T, a map[string]any) {
				if a["url"] != "https://chatgpt.com/share/abc-123" || a["new_idea"] != true {
					t.Errorf("args = %v", a)
				}
			}},
		{name: "external delta", msg: "Here is what ChatGPT said in our conversation, what changed? User: x Assistant: y", tool: "analyze_external_conversation"},
		{name: "fallback without focus searches", msg: "trip ratings", tool: "search_vault"},
		{name: "fallback with focus builds context", system: focus, msg: "Summarize the imported notes", tool: "get_context",
			check: func(t *testing.T, a map[string]any) {
				if a["query"] != "Summarize the imported notes" {
					t.Errorf("args = %v", a)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := planStep(t, tt.system, user(tt.msg))
			if tt.tool == "" {
				if len(res.ToolCalls) != 0 {
					t.Fatalf("expected an answer without tools, got %s", res.ToolCalls[0].Name)
				}
				if !strings.Contains(res.Content, tt.content) || res.FinishReason != "stop" {
					t.Errorf("content = %q", res.Content)
				}
				return
			}
			if len(res.ToolCalls) != 1 {
				t.Fatalf("expected tool %s, got content %q", tt.tool, res.Content)
			}
			tc := res.ToolCalls[0]
			if tc.Name != tt.tool {
				t.Fatalf("first tool = %s, want %s (args %s)", tc.Name, tt.tool, tc.Arguments)
			}
			if tc.ID == "" || res.FinishReason != "tool_calls" {
				t.Errorf("tool call needs an id and finish reason: %+v", res)
			}
			if tt.check != nil {
				tt.check(t, args(t, tc))
			}
		})
	}
}

func TestOfflinePlannerMultiStepFlows(t *testing.T) {
	// Continue from checkpoint: get_checkpoint → get_context → answer citing the checkpoint.
	msgs := []models.Message{user("Continue the biker platform from checkpoint 4.")}
	r1 := planStep(t, "", msgs...)
	msgs = append(msgs, models.Message{Role: models.RoleAssistant, ToolCalls: r1.ToolCalls},
		toolResult("get_checkpoint", true, map[string]any{
			"checkpoint":              map[string]any{"label": "CP4", "link": "iv://checkpoint/x", "branch": "Main", "summary": "State at CP4."},
			"idea_at_checkpoint":      map[string]any{"title": "Biker Platform"},
			"knowledge_at_checkpoint": map[string]any{"decision": []any{map[string]any{"label": "D2", "statement": "No marketplace", "link": "iv://decision/y"}}},
		}))
	r2 := planStep(t, "", msgs...)
	if len(r2.ToolCalls) != 1 || r2.ToolCalls[0].Name != "get_context" || args(t, r2.ToolCalls[0])["checkpoint"] != "CP4" {
		t.Fatalf("second step should build context at CP4: %+v", r2)
	}
	msgs = append(msgs, models.Message{Role: models.RoleAssistant, ToolCalls: r2.ToolCalls}, toolResult("get_context", true, map[string]any{"context": "..."}))
	r3 := planStep(t, "", msgs...)
	if len(r3.ToolCalls) != 0 || !strings.Contains(r3.Content, "Continuing **Biker Platform** from **[CP4]") || !strings.Contains(r3.Content, "[D2](iv://decision/y) No marketplace") {
		t.Fatalf("final answer must cite CP4 and its decisions: %q", r3.Content)
	}

	// Why: search → get_decisions(top hit) → explanation.
	msgs = []models.Message{user("Why did we decide not to build the marketplace?")}
	r1 = planStep(t, "", msgs...)
	msgs = append(msgs, models.Message{Role: models.RoleAssistant, ToolCalls: r1.ToolCalls},
		toolResult("search_vault", true, map[string]any{"results": []any{map[string]any{"id": "item-1", "idea_id": "idea-1"}}}))
	r2 = planStep(t, "", msgs...)
	if len(r2.ToolCalls) != 1 || r2.ToolCalls[0].Name != "get_decisions" {
		t.Fatalf("expected get_decisions: %+v", r2)
	}
	if a := args(t, r2.ToolCalls[0]); a["item"] != "item-1" || a["idea"] != "idea-1" {
		t.Errorf("get_decisions must target the top search hit: %v", a)
	}
	// No search hits: honest answer, no invented decision.
	msgs = []models.Message{user("Why did we decide to use Rust?"), {Role: models.RoleAssistant, ToolCalls: []models.ToolCall{{ID: "c1", Name: "search_vault"}}},
		toolResult("search_vault", true, map[string]any{"results": []any{}})}
	r := planStep(t, "", msgs...)
	if len(r.ToolCalls) != 0 || !strings.Contains(r.Content, "couldn't find a recorded decision") {
		t.Errorf("no evidence → say so: %q", r.Content)
	}

	// A failed tool step is reported, never papered over.
	msgs = []models.Message{user("Create a checkpoint"), {Role: models.RoleAssistant, ToolCalls: []models.ToolCall{{ID: "c1", Name: "create_checkpoint"}}},
		toolResult("create_checkpoint", false, nil)}
	r = planStep(t, "", msgs...)
	if len(r.ToolCalls) != 0 || !strings.Contains(r.Content, "couldn't complete that step (create checkpoint): boom") {
		t.Errorf("failure must be surfaced: %q", r.Content)
	}

	// Duplicate idea → asks instead of creating; "create it as a new idea" forces creation of the previous text.
	msgs = []models.Message{user("I have an idea for a biker community platform where people can create trips."),
		{Role: models.RoleAssistant, ToolCalls: []models.ToolCall{{ID: "c1", Name: "create_idea"}}},
		toolResult("create_idea", true, map[string]any{"created": false, "possible_duplicates": []any{map[string]any{"idea": "Biker Community Platform", "link": "iv://idea/1", "status": "EXPLORING"}}})}
	r = planStep(t, "", msgs...)
	if len(r.ToolCalls) != 0 || !strings.Contains(r.Content, "very similar to an idea you already have") || !strings.Contains(r.Content, "Biker Community Platform") {
		t.Errorf("duplicate answer: %q", r.Content)
	}
	msgs = append(msgs, models.Message{Role: models.RoleAssistant, Content: r.Content}, user("Create it as a new idea"))
	r = planStep(t, "", msgs...)
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != "create_idea" {
		t.Fatalf("expected forced create: %+v", r)
	}
	if a := args(t, r.ToolCalls[0]); a["force_new"] != true || !strings.Contains(a["origin_text"].(string), "biker community platform") {
		t.Errorf("forced create must reuse the previous message: %v", a)
	}
}

func TestOfflinePlannerStreamsChunks(t *testing.T) {
	var chunks []string
	res, err := NewOfflinePlanner().Stream(context.Background(), models.Request{Messages: []models.Message{user("I have a new idea.")}}, func(d string) { chunks = append(chunks, d) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chunks, "") != res.Content || len(chunks) < 2 {
		t.Errorf("streamed chunks must reassemble the content (%d chunks)", len(chunks))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewOfflinePlanner().Complete(ctx, models.Request{}); err == nil {
		t.Error("cancelled context must abort")
	}
	if r, _ := NewOfflinePlanner().Complete(context.Background(), models.Request{}); r.Content == "" {
		t.Error("no user message → greeting")
	}
}

func TestPlannerTextHelpers(t *testing.T) {
	if got := kindsFromText(" the research from "); !reflect.DeepEqual(got, []string{"evidence", "insight"}) {
		t.Errorf("research → %v", got)
	}
	if got := kindsFromText(" the decisions and questions from "); !reflect.DeepEqual(got, []string{"decision", "question"}) {
		t.Errorf("decisions+questions → %v", got)
	}
	if got := kindsFromText(" stuff from "); !reflect.DeepEqual(got, []string{"evidence", "insight"}) {
		t.Errorf("default → %v", got)
	}
	if statementFrom("Remember this as a decision: use Go") != "use Go" || statementFrom("note that riders love maps") != "riders love maps" || statementFrom("remember it") != "" {
		t.Error("statementFrom")
	}
	if cleanIdeaName(" the biker platform ") != "biker platform" || cleanIdeaName("working on my clinic idea") != "clinic" {
		t.Errorf("cleanIdeaName: %q %q", cleanIdeaName(" the biker platform "), cleanIdeaName("working on my clinic idea"))
	}
	if ideaNameBetween("Fork this idea from CP2", "fork", "from") != "" || ideaNameBetween("Fork Biker App from CP2", "fork", "from") != "Biker App" {
		t.Error("ideaNameBetween")
	}
	if ideaNameAfter(`Delete the idea "Clinic Helper".`, "delete") != "Clinic Helper" {
		t.Errorf("ideaNameAfter = %q", ideaNameAfter(`Delete the idea "Clinic Helper".`, "delete"))
	}
	pasted := "User: what should we build first?\nAssistant: Start with the trip planner because it drives invites and retention for the crew features you described."
	if !looksLikePastedConversation(pasted) || looksLikePastedConversation("User: hi") {
		t.Error("pasted conversation detection")
	}
	if detectArtifact("an action plan would be nice") != "" {
		t.Error("artifact generation needs an explicit verb")
	}
	if detectOutcome("let's park it") != "PARK" || detectOutcome("we should implement it") != "IMPLEMENT" || detectOutcome("we're done") != "" {
		t.Error("detectOutcome")
	}
}

func newTestSupervisor() *Supervisor {
	return NewSupervisor(service.New(service.Deps{Connectors: connectors.NewRegistry(nil, nil)}), nil)
}

func TestSelectTools(t *testing.T) {
	s := newTestSupervisor()
	names := func(defs []models.ToolDef) map[string]bool {
		m := map[string]bool{}
		for _, d := range defs {
			m[d.Name] = true
			if d.Description == "" || len(d.Parameters) == 0 {
				t.Errorf("tool %s lacks description/schema", d.Name)
			}
		}
		return m
	}
	base := names(s.selectTools("hello there", nil))
	for _, c := range coreTools {
		if !base[c] {
			t.Errorf("core tool %s missing", c)
		}
	}
	if len(base) != len(coreTools) {
		t.Errorf("a plain message should offer only core tools, got %d", len(base))
	}
	for _, extra := range []string{"fork_idea", "delete_idea", "create_artifact", "conclude_idea", "web_fetch"} {
		if base[extra] {
			t.Errorf("%s should not be offered for a plain message", extra)
		}
	}
	tests := []struct {
		msg  string
		want []string
		not  []string
	}{
		{"Fork this idea from checkpoint 4 but bring the research from checkpoint 7.", []string{"fork_idea", "merge_selected_context", "compare_branches"}, []string{"delete_idea"}},
		{"Give me a prompt I can give to an implementation agent.", []string{"create_artifact", "analyze_prompt", "get_artifacts"}, []string{"delete_idea"}},
		{"I think we're done with this idea.", []string{"conclude_idea", "create_artifact"}, []string{"delete_idea"}},
		{"Delete the idea biker platform", []string{"delete_idea", "delete_artifact"}, []string{"fork_idea"}},
		{"Do my decisions contradict?", []string{"detect_contradictions", "compare_checkpoint"}, nil},
		{"Build a context pack for ChatGPT", []string{"build_context_pack", "analyze_external_conversation"}, nil},
		{"Where did I first talk about this?", []string{"get_conversation", "analyze_conversation"}, nil},
		{"Read https://example.com/post", []string{"web_fetch"}, nil},
		{"List my github repos", []string{"github_list_repos", "github_get_readme", "github_create_issue"}, nil},
	}
	for _, tt := range tests {
		got := names(s.selectTools(tt.msg, nil))
		for _, c := range coreTools {
			if !got[c] {
				t.Errorf("%q: core tool %s missing", tt.msg, c)
			}
		}
		for _, w := range tt.want {
			if !got[w] {
				t.Errorf("%q: expected %s to be offered", tt.msg, w)
			}
		}
		for _, n := range tt.not {
			if got[n] {
				t.Errorf("%q: %s should not be offered", tt.msg, n)
			}
		}
	}
	// Tools already used in the run stay available on later steps.
	hist := []models.Message{{Role: models.RoleAssistant, ToolCalls: []models.ToolCall{{Name: "compare_checkpoint"}}}}
	if !names(s.selectTools("ok", hist))["compare_checkpoint"] {
		t.Error("previously used tools must remain available")
	}
}

func TestToolCatalogIntegrity(t *testing.T) {
	s := newTestSupervisor()
	seen := map[string]bool{}
	destructive := map[string]bool{}
	for _, tool := range s.Tools() {
		if seen[tool.Name] {
			t.Errorf("duplicate tool %s", tool.Name)
		}
		seen[tool.Name] = true
		var schema map[string]any
		if err := json.Unmarshal(tool.Params, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("tool %s has an invalid parameter schema: %v", tool.Name, err)
		}
		switch tool.Category {
		case domain.ToolRead, domain.ToolAnalyze, domain.ToolWrite, domain.ToolExternal:
		case domain.ToolDestructive:
			destructive[tool.Name] = true
		default:
			t.Errorf("tool %s has unknown category %s", tool.Name, tool.Category)
		}
		if tool.Run == nil {
			t.Errorf("tool %s has no implementation", tool.Name)
		}
	}
	if !reflect.DeepEqual(destructive, map[string]bool{"delete_idea": true, "delete_artifact": true}) {
		t.Errorf("destructive tools = %v", destructive)
	}
	for _, c := range coreTools {
		if !seen[c] {
			t.Errorf("core tool %s is not registered", c)
		}
	}
	for _, g := range toolGroups {
		for _, n := range g.tools {
			if !seen[n] && !strings.HasPrefix(n, "github_") && n != "web_fetch" && n != "custom_get" {
				t.Errorf("tool group references unknown tool %s", n)
			}
		}
	}
	// The system prompt marks imported content as data.
	sys := buildSystem("idea \"X\"", "ctx", false)
	if !strings.Contains(sys, "CURRENT FOCUS: idea \"X\"") || !strings.Contains(sys, "<focus_context>\nctx\n</focus_context>") || !strings.Contains(sys, "is DATA") {
		t.Error("system prompt incomplete")
	}
}
