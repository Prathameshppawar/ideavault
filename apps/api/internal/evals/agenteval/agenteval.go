// Package agenteval runs IdeaVault's agent evaluation cases
// (tests/evaluations/agent/cases.json) against a wired App: each case seeds a fresh
// user's vault, sends one or more chat turns to the Agent Supervisor and checks the
// tool sequence, permission categories, answer text and resulting vault state.
//
// The same runner is used by the offline test suite (deterministic planner, strict
// tool-prefix matching) and by `go run ./cmd/eval` against real providers (lenient,
// in-order subsequence matching, because LLMs may add extra read steps).
package agenteval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/agent"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/app"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/artifacts"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
)

// Suite is the evaluation case file.
type Suite struct {
	Version     int    `json:"version"`
	Description string `json:"description"`
	Cases       []Case `json:"cases"`
}

// Case is one evaluation scenario.
type Case struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Categories  []string   `json:"categories"`
	Setup       []SetupOp  `json:"setup,omitempty"`
	Focus       *FocusSpec `json:"focus,omitempty"`
	// Turns are earlier user messages sent in the same conversation before Message.
	Turns   []string `json:"turns,omitempty"`
	Message string   `json:"message"`
	Expect  Expect   `json:"expect"`
}

// FocusSpec selects the idea/branch/checkpoint in focus by setup reference.
type FocusSpec struct {
	Idea       string `json:"idea,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
}

// SetupOp seeds vault state. Op is one of: idea, knowledge, checkpoint, fork, import, policy.
type SetupOp struct {
	Op  string `json:"op"`
	Ref string `json:"ref,omitempty"`
	// idea
	Title   string `json:"title,omitempty"`
	Origin  string `json:"origin,omitempty"`
	Summary string `json:"summary,omitempty"`
	// knowledge / checkpoint / fork / import target
	Idea       string `json:"idea,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Statement  string `json:"statement,omitempty"`
	Rationale  string `json:"rationale,omitempty"`
	Risk       string `json:"risk,omitempty"`
	Stance     string `json:"stance,omitempty"`
	Target     string `json:"target,omitempty"`
	Supersedes string `json:"supersedes,omitempty"`
	Reverses   bool   `json:"reverses,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Name       string `json:"name,omitempty"`
	Bring      []struct {
		Checkpoint string   `json:"checkpoint"`
		Kinds      []string `json:"kinds"`
	} `json:"bring,omitempty"`
	// import: a file under tests/fixtures (e.g. "imports/injection/malicious_chatgpt.json").
	Fixture string `json:"fixture,omitempty"`
	NewIdea bool   `json:"new_idea,omitempty"`
	// policy
	Policy map[string]any `json:"policy,omitempty"`
}

// Expect holds the checks for the final turn.
type Expect struct {
	// Status is the final run status (default COMPLETED).
	Status string `json:"status,omitempty"`
	// ToolPrefix: requested tools must start with these (strict) or contain them in order (lenient).
	ToolPrefix []string `json:"tool_prefix"`
	// MaxTools bounds the number of requested tool calls (-1 = unbounded; 0 means "answer without tools").
	MaxTools *int `json:"max_tools,omitempty"`
	// Categories lists the permission categories executed tools may belong to.
	Categories []string `json:"categories,omitempty"`
	// ForbiddenTools must never be requested.
	ForbiddenTools     []string    `json:"forbidden_tools,omitempty"`
	ContentContains    []string    `json:"content_contains,omitempty"`
	ContentNotContains []string    `json:"content_not_contains,omitempty"`
	Assertions         []Assertion `json:"assertions,omitempty"`
}

// Assertion checks vault state after the run. See Runner.assert for the supported types.
type Assertion struct {
	Type         string   `json:"type"`
	Idea         string   `json:"idea,omitempty"`       // setup ref
	IdeaTitle    string   `json:"idea_title,omitempty"` // or exact title (ideas created by the agent)
	Branch       string   `json:"branch,omitempty"`     // setup ref
	BranchName   string   `json:"branch_name,omitempty"`
	Checkpoint   string   `json:"checkpoint,omitempty"`
	Item         string   `json:"item,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Tool         string   `json:"tool,omitempty"`
	Arg          string   `json:"arg,omitempty"`
	RelType      string   `json:"rel_type,omitempty"`
	ArtifactType string   `json:"artifact_type,omitempty"`
	Status       string   `json:"status,omitempty"`
	Statement    string   `json:"statement,omitempty"`
	Origin       string   `json:"origin,omitempty"`
	Contains     []string `json:"contains,omitempty"`
	StartsWith   string   `json:"starts_with,omitempty"`
	Live         bool     `json:"live,omitempty"`
	HasSource    bool     `json:"has_source_message,omitempty"`
	Equals       *int     `json:"equals,omitempty"`
	Min          *int     `json:"min,omitempty"`
	Value        string   `json:"value,omitempty"`
	CitesKnown   bool     `json:"cites_only_known_labels,omitempty"`
	MinCited     int      `json:"min_cited,omitempty"`
}

// Check is one evaluated expectation.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// ToolCall summarises a requested tool call.
type ToolCall struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Status   string `json:"status"`
	Args     string `json:"args"`
}

// CaseResult is the outcome of one case.
type CaseResult struct {
	ID           string     `json:"id"`
	Description  string     `json:"description"`
	Categories   []string   `json:"categories"`
	Passed       bool       `json:"passed"`
	Error        string     `json:"error,omitempty"`
	Status       string     `json:"status"`
	Tools        []ToolCall `json:"tools"`
	Checks       []Check    `json:"checks"`
	Content      string     `json:"content"`
	LatencyMS    int64      `json:"latency_ms"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
}

// CategoryScore aggregates results per evaluation category.
type CategoryScore struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
}

// Report is the full evaluation output.
type Report struct {
	GeneratedAt time.Time                `json:"generated_at"`
	Mode        string                   `json:"mode"` // strict | lenient
	Model       string                   `json:"model"`
	Total       int                      `json:"total"`
	Passed      int                      `json:"passed"`
	Failed      int                      `json:"failed"`
	PassRate    float64                  `json:"pass_rate"`
	ByCategory  map[string]CategoryScore `json:"by_category"`
	Cases       []CaseResult             `json:"cases"`
}

// RepoRoot locates the repository root (the directory holding db/migrations and tests/).
func RepoRoot() (string, error) {
	dir, err := db.FindMigrationsDir("")
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(dir)), nil
}

// DefaultSuitePath is tests/evaluations/agent/cases.json under the repository root.
func DefaultSuitePath() (string, error) {
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tests", "evaluations", "agent", "cases.json"), nil
}

// LoadSuite reads and validates a case file.
func LoadSuite(path string) (*Suite, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Suite
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if c.ID == "" || seen[c.ID] {
			return nil, fmt.Errorf("case id %q is empty or duplicated", c.ID)
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Message) == "" || len(c.Categories) == 0 {
			return nil, fmt.Errorf("case %s needs a message and categories", c.ID)
		}
	}
	return &s, nil
}

// WriteReport writes the report as indented JSON, creating parent directories.
func WriteReport(path string, r *Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Runner executes cases against an App.
type Runner struct {
	App *app.App
	// Pin optionally forces "provider/model" for every chat turn.
	Pin string
	// Strict requires the tool sequence to start with Expect.ToolPrefix exactly
	// (offline planner); otherwise the prefix tools must appear in order.
	Strict bool
	// Timeout bounds each case (default 3 minutes).
	Timeout time.Duration
}

// Run executes every case and aggregates a report.
func (r *Runner) Run(ctx context.Context, s *Suite) *Report {
	rep := &Report{GeneratedAt: time.Now().UTC(), Mode: "lenient", Model: r.Pin, ByCategory: map[string]CategoryScore{}}
	if r.Strict {
		rep.Mode = "strict"
	}
	if rep.Model == "" {
		rep.Model = "routed"
	}
	for _, c := range s.Cases {
		res := r.RunCase(ctx, c)
		rep.Cases = append(rep.Cases, res)
		rep.Total++
		if res.Passed {
			rep.Passed++
		} else {
			rep.Failed++
		}
		for _, cat := range c.Categories {
			sc := rep.ByCategory[cat]
			sc.Total++
			if res.Passed {
				sc.Passed++
			}
			rep.ByCategory[cat] = sc
		}
	}
	if rep.Total > 0 {
		rep.PassRate = float64(rep.Passed) / float64(rep.Total)
	}
	return rep
}

// caseState holds references created during setup.
type caseState struct {
	user        uuid.UUID
	ideas       map[string]uuid.UUID
	branches    map[string]uuid.UUID
	items       map[string]uuid.UUID
	checkpoints map[string]uuid.UUID
}

func (st *caseState) idea(ref string) (uuid.UUID, error) {
	if id, ok := st.ideas[ref]; ok {
		return id, nil
	}
	return uuid.Nil, fmt.Errorf("unknown idea ref %q", ref)
}

// RunCase seeds a fresh user, runs the turns and evaluates the expectations.
func (r *Runner) RunCase(ctx context.Context, c Case) (out CaseResult) {
	out = CaseResult{ID: c.ID, Description: c.Description, Categories: c.Categories, Tools: []ToolCall{}, Checks: []Check{}}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			out.Passed, out.Error = false, fmt.Sprintf("panic: %v", p)
		}
	}()
	st, err := r.setup(ctx, c)
	if err != nil {
		out.Error = "setup: " + err.Error()
		return out
	}
	req := agent.ChatRequest{Model: r.Pin}
	if c.Focus != nil {
		if c.Focus.Idea != "" {
			id, err := st.idea(c.Focus.Idea)
			if err != nil {
				out.Error = err.Error()
				return out
			}
			req.IdeaID = &id
		}
		if c.Focus.Branch != "" {
			id, ok := st.branches[c.Focus.Branch]
			if !ok {
				out.Error = "unknown focus branch " + c.Focus.Branch
				return out
			}
			req.BranchID = &id
		}
		if c.Focus.Checkpoint != "" {
			id, ok := st.checkpoints[c.Focus.Checkpoint]
			if !ok {
				out.Error = "unknown focus checkpoint " + c.Focus.Checkpoint
				return out
			}
			req.CheckpointID = &id
		}
	}
	noop := func(agent.Event) {}
	for _, turn := range c.Turns {
		req.Message = turn
		res, err := r.App.Supervisor.Chat(ctx, st.user, req, noop)
		if err != nil {
			out.Error = "turn: " + err.Error()
			return out
		}
		req.ConversationID = &res.ConversationID
	}
	req.Message = c.Message
	start := time.Now()
	res, err := r.App.Supervisor.Chat(ctx, st.user, req, noop)
	out.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		out.Error = "chat: " + err.Error()
		return out
	}
	out.Status = string(res.Status)
	out.Content = res.Content
	run, err := r.App.Store.GetAgentRun(ctx, st.user, res.RunID)
	if err != nil {
		out.Error = "load run: " + err.Error()
		return out
	}
	out.InputTokens, out.OutputTokens = run.InputTokens, run.OutputTokens
	for _, tc := range run.ToolCalls {
		out.Tools = append(out.Tools, ToolCall{Name: tc.ToolName, Category: string(tc.Category), Status: tc.Status, Args: string(tc.Arguments)})
	}
	r.evaluate(ctx, c, st, res, &out)
	out.Passed = out.Error == ""
	for _, ch := range out.Checks {
		if !ch.Passed {
			out.Passed = false
		}
	}
	return out
}

func (r *Runner) setup(ctx context.Context, c Case) (*caseState, error) {
	store, svc := r.App.Store, r.App.Service
	email := fmt.Sprintf("eval-%s-%s@ideavault.test", regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(c.ID), "-"), uuid.NewString()[:8])
	u, err := store.CreateUser(ctx, email, "Eval", "$argon2id$v=19$m=65536,t=2,p=2$ZXZhbA$ZXZhbA") // never used to log in
	if err != nil {
		return nil, err
	}
	st := &caseState{user: u.ID, ideas: map[string]uuid.UUID{}, branches: map[string]uuid.UUID{}, items: map[string]uuid.UUID{}, checkpoints: map[string]uuid.UUID{}}
	actor := service.UserActor(u.ID)
	root, _ := RepoRoot()
	for i, op := range c.Setup {
		fail := func(err error) (*caseState, error) {
			return nil, fmt.Errorf("op %d (%s %s): %w", i, op.Op, op.Ref, err)
		}
		switch op.Op {
		case "idea":
			res, err := svc.CreateIdea(ctx, actor, service.CreateIdeaInput{Title: op.Title, OriginText: op.Origin, Summary: op.Summary})
			if err != nil {
				return fail(err)
			}
			st.ideas[op.Ref] = res.Idea.ID
			st.branches[op.Ref+"/main"] = res.Branch.ID
		case "knowledge":
			ideaID, err := st.idea(op.Idea)
			if err != nil {
				return fail(err)
			}
			in := service.RecordKnowledgeInput{IdeaID: ideaID, Kind: domain.KnowledgeKind(op.Kind), Statement: op.Statement, Rationale: op.Rationale, Risk: op.Risk,
				Stance: op.Stance, Reverses: op.Reverses}
			if op.Branch != "" {
				b, ok := st.branches[op.Branch]
				if !ok {
					return fail(fmt.Errorf("unknown branch %q", op.Branch))
				}
				in.BranchID = &b
			}
			if op.Supersedes != "" {
				id, ok := st.items[op.Supersedes]
				if !ok {
					return fail(fmt.Errorf("unknown item %q", op.Supersedes))
				}
				in.Supersedes = &id
			}
			if op.Target != "" {
				id, ok := st.items[op.Target]
				if !ok {
					return fail(fmt.Errorf("unknown item %q", op.Target))
				}
				in.TargetItemID = &id
			}
			it, err := svc.RecordKnowledge(ctx, actor, in)
			if err != nil {
				return fail(err)
			}
			if op.Ref != "" {
				st.items[op.Ref] = it.ID
			}
		case "checkpoint":
			ideaID, err := st.idea(op.Idea)
			if err != nil {
				return fail(err)
			}
			in := service.CreateCheckpointInput{IdeaID: ideaID, Title: op.Title}
			if op.Branch != "" {
				b := st.branches[op.Branch]
				in.BranchID = &b
			}
			cp, err := svc.CreateCheckpoint(ctx, actor, in)
			if err != nil {
				return fail(err)
			}
			st.checkpoints[op.Ref] = cp.ID
		case "fork":
			cpID, ok := st.checkpoints[op.Checkpoint]
			if !ok {
				return fail(fmt.Errorf("unknown checkpoint %q", op.Checkpoint))
			}
			in := service.ForkInput{CheckpointID: cpID, Name: op.Name}
			for _, b := range op.Bring {
				id := st.checkpoints[b.Checkpoint]
				sel := service.Selection{CheckpointID: &id}
				for _, k := range b.Kinds {
					sel.Kinds = append(sel.Kinds, domain.KnowledgeKind(k))
				}
				in.Selections = append(in.Selections, sel)
			}
			res, err := svc.ForkFromCheckpoint(ctx, actor, in)
			if err != nil {
				return fail(err)
			}
			st.branches[op.Ref] = res.Branch.ID
		case "import":
			data, err := os.ReadFile(filepath.Join(root, "tests", "fixtures", filepath.FromSlash(op.Fixture)))
			if err != nil {
				return fail(err)
			}
			in := service.ImportNowInput{CreateImportInput: service.CreateImportInput{SourceKind: "file", Filename: filepath.Base(op.Fixture), Data: data}, Extract: true, NewIdea: op.NewIdea}
			if op.Idea != "" {
				id, err := st.idea(op.Idea)
				if err != nil {
					return fail(err)
				}
				in.IdeaID = &id
			}
			res, err := svc.ImportNow(ctx, actor, in)
			if err != nil {
				return fail(err)
			}
			if op.Ref != "" && len(res.Ideas) > 0 {
				st.ideas[op.Ref] = res.Ideas[0].ID
			}
		case "policy":
			if _, err := store.MergeSettings(ctx, u.ID, map[string]any{"agent_policy": op.Policy}); err != nil {
				return fail(err)
			}
		default:
			return fail(errors.New("unknown setup op"))
		}
	}
	// Embed everything now so semantic retrieval is deterministic (no background worker).
	if _, err := svc.BackfillEmbeddings(ctx, u.ID, 2000); err != nil {
		return nil, err
	}
	return st, nil
}

func (r *Runner) evaluate(ctx context.Context, c Case, st *caseState, res *agent.ChatResult, out *CaseResult) {
	add := func(name string, ok bool, detail string, args ...any) {
		ch := Check{Name: name, Passed: ok}
		if !ok {
			ch.Detail = fmt.Sprintf(detail, args...)
		}
		out.Checks = append(out.Checks, ch)
	}
	ex := c.Expect
	wantStatus := ex.Status
	if wantStatus == "" {
		wantStatus = string(domain.RunCompleted)
	}
	add("status", out.Status == wantStatus, "status %s, want %s", out.Status, wantStatus)
	var names []string
	for _, t := range out.Tools {
		names = append(names, t.Name)
	}
	if len(ex.ToolPrefix) > 0 {
		ok := hasPrefix(names, ex.ToolPrefix)
		if !r.Strict {
			ok = isSubsequence(names, ex.ToolPrefix)
		}
		add("tool_sequence", ok, "tools %v, want prefix %v", names, ex.ToolPrefix)
	}
	if ex.MaxTools != nil {
		add("max_tools", len(names) <= *ex.MaxTools, "%d tool calls, max %d (%v)", len(names), *ex.MaxTools, names)
	}
	if len(ex.Categories) > 0 {
		allowed := map[string]bool{}
		for _, cat := range ex.Categories {
			allowed[cat] = true
		}
		var bad []string
		for _, t := range out.Tools {
			if t.Status == "SUCCEEDED" && !allowed[t.Category] {
				bad = append(bad, t.Name+"("+t.Category+")")
			}
		}
		add("tool_categories", len(bad) == 0, "executed tools outside %v: %v", ex.Categories, bad)
	}
	if len(ex.ForbiddenTools) > 0 {
		var hit []string
		for _, n := range names {
			for _, f := range ex.ForbiddenTools {
				if n == f {
					hit = append(hit, n)
				}
			}
		}
		add("forbidden_tools", len(hit) == 0, "forbidden tools requested: %v", hit)
	}
	lower := strings.ToLower(out.Content)
	for _, s := range ex.ContentContains {
		add("content_contains:"+s, strings.Contains(lower, strings.ToLower(s)), "answer does not mention %q", s)
	}
	for _, s := range ex.ContentNotContains {
		add("content_not_contains:"+s, !strings.Contains(lower, strings.ToLower(s)), "answer mentions %q", s)
	}
	for i, a := range ex.Assertions {
		name := fmt.Sprintf("assert[%d]:%s", i, a.Type)
		ok, detail := r.assert(ctx, st, res, out, a)
		add(name, ok, "%s", detail)
	}
}

func hasPrefix(got, want []string) bool {
	if len(got) < len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func isSubsequence(got, want []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

func compare(n int, a Assertion) (bool, string) {
	switch {
	case a.Equals != nil && n != *a.Equals:
		return false, fmt.Sprintf("got %d, want %d", n, *a.Equals)
	case a.Min != nil && n < *a.Min:
		return false, fmt.Sprintf("got %d, want at least %d", n, *a.Min)
	}
	return true, ""
}

// resolveIdea finds the idea for an assertion by setup ref or exact title.
func (r *Runner) resolveIdea(ctx context.Context, st *caseState, a Assertion) (*domain.Idea, error) {
	if a.Idea != "" {
		id, err := st.idea(a.Idea)
		if err != nil {
			return nil, err
		}
		return r.App.Store.GetIdea(ctx, st.user, id)
	}
	ideas, _, err := r.App.Store.ListIdeas(ctx, postgres.IdeaFilter{UserID: st.user, Limit: 500})
	if err != nil {
		return nil, err
	}
	for i := range ideas {
		if ideas[i].Title == a.IdeaTitle {
			return &ideas[i], nil
		}
	}
	return nil, fmt.Errorf("no idea titled %q", a.IdeaTitle)
}

func (r *Runner) resolveBranch(ctx context.Context, st *caseState, idea *domain.Idea, a Assertion) (*uuid.UUID, error) {
	switch {
	case a.Branch != "":
		id, ok := st.branches[a.Branch]
		if !ok {
			return nil, fmt.Errorf("unknown branch ref %q", a.Branch)
		}
		return &id, nil
	case a.BranchName != "":
		b, err := r.App.Store.FindBranch(ctx, st.user, idea.ID, a.BranchName)
		if err != nil {
			return nil, err
		}
		return &b.ID, nil
	}
	return nil, nil
}

// assert evaluates one state assertion. Supported types: idea_count, branch_count,
// checkpoint_count, knowledge_count, latest_knowledge, item_status, idea, idea_status,
// artifact, context_pack_count, relationship_count, focus, pending_confirmation,
// tool_status, tool_arg.
func (r *Runner) assert(ctx context.Context, st *caseState, res *agent.ChatResult, out *CaseResult, a Assertion) (bool, string) {
	store := r.App.Store
	fail := func(err error) (bool, string) { return false, err.Error() }
	switch a.Type {
	case "idea_count":
		_, total, err := store.ListIdeas(ctx, postgres.IdeaFilter{UserID: st.user, Limit: 1})
		if err != nil {
			return fail(err)
		}
		return compare(total, a)
	case "branch_count", "checkpoint_count", "context_pack_count":
		idea, err := r.resolveIdea(ctx, st, a)
		if err != nil {
			return fail(err)
		}
		var n int
		switch a.Type {
		case "branch_count":
			bs, err := store.ListBranches(ctx, st.user, idea.ID)
			if err != nil {
				return fail(err)
			}
			n = len(bs)
		case "checkpoint_count":
			cps, err := store.ListCheckpoints(ctx, st.user, idea.ID, nil)
			if err != nil {
				return fail(err)
			}
			n = len(cps)
		default:
			ps, err := store.ListContextPacks(ctx, st.user, &idea.ID, 500)
			if err != nil {
				return fail(err)
			}
			n = len(ps)
		}
		return compare(n, a)
	case "knowledge_count", "latest_knowledge":
		idea, err := r.resolveIdea(ctx, st, a)
		if err != nil {
			return fail(err)
		}
		f := postgres.KnowledgeFilter{UserID: st.user, IdeaID: &idea.ID, LiveOnly: a.Live, Limit: 1000}
		if f.BranchID, err = r.resolveBranch(ctx, st, idea, a); err != nil {
			return fail(err)
		}
		if a.Kind != "" {
			f.Kinds = []domain.KnowledgeKind{domain.KnowledgeKind(a.Kind)}
		}
		items, err := store.ListKnowledge(ctx, f)
		if err != nil {
			return fail(err)
		}
		if a.Type == "knowledge_count" {
			for _, s := range a.Contains {
				found := false
				for _, it := range items {
					if strings.Contains(strings.ToLower(it.Statement), strings.ToLower(s)) {
						found = true
					}
				}
				if !found {
					return false, fmt.Sprintf("no %s item containing %q", a.Kind, s)
				}
			}
			return compare(len(items), a)
		}
		if len(items) == 0 {
			return false, "no knowledge recorded"
		}
		it := items[0] // newest first
		var problems []string
		if a.Statement != "" && it.Statement != a.Statement {
			problems = append(problems, fmt.Sprintf("statement %q", it.Statement))
		}
		if a.Status != "" && it.Status != a.Status {
			problems = append(problems, "status "+it.Status)
		}
		if a.Origin != "" && string(it.Origin) != a.Origin {
			problems = append(problems, "origin "+string(it.Origin))
		}
		if a.HasSource && (it.SourceMessageID == nil || it.SourceExcerpt == "") {
			problems = append(problems, "no source message/excerpt")
		}
		if a.Kind == "assumption" && a.Value != "" && (it.Assumption == nil || it.Assumption.Risk != a.Value) {
			problems = append(problems, "risk mismatch")
		}
		return len(problems) == 0, strings.Join(problems, "; ")
	case "item_status":
		id, ok := st.items[a.Item]
		if !ok {
			return false, "unknown item ref " + a.Item
		}
		it, err := store.GetKnowledge(ctx, st.user, id)
		if err != nil {
			return fail(err)
		}
		return it.Status == a.Status, "status " + it.Status
	case "idea", "idea_status":
		idea, err := r.resolveIdea(ctx, st, a)
		if err != nil {
			return fail(err)
		}
		if a.Status != "" && string(idea.Status) != a.Status {
			return false, "status " + string(idea.Status)
		}
		for _, s := range a.Contains {
			if !strings.Contains(strings.ToLower(idea.OriginText), strings.ToLower(s)) {
				return false, fmt.Sprintf("origin text %q lacks %q", idea.OriginText, s)
			}
		}
		if a.HasSource && idea.OriginConversationID == nil {
			return false, "origin conversation not linked"
		}
		return true, ""
	case "artifact":
		idea, err := r.resolveIdea(ctx, st, a)
		if err != nil {
			return fail(err)
		}
		arts, err := store.ListArtifacts(ctx, postgres.ArtifactFilter{UserID: st.user, IdeaID: &idea.ID, Type: domain.ArtifactType(a.ArtifactType)})
		if err != nil {
			return fail(err)
		}
		if ok, d := compare(len(arts), a); !ok || len(arts) == 0 {
			return false, "artifacts: " + d
		}
		full, err := store.GetArtifact(ctx, st.user, arts[0].ID)
		if err != nil {
			return fail(err)
		}
		if a.StartsWith != "" && !strings.HasPrefix(full.ContentMarkdown, a.StartsWith) {
			return false, "markdown does not start with " + a.StartsWith
		}
		cited, _ := artifacts.CitedLabels(full.ContentMarkdown)
		if len(cited) < a.MinCited {
			return false, fmt.Sprintf("cites %d labels, want at least %d", len(cited), a.MinCited)
		}
		if a.CitesKnown {
			items, err := store.ListKnowledge(ctx, postgres.KnowledgeFilter{UserID: st.user, IdeaID: &idea.ID, Limit: 2000})
			if err != nil {
				return fail(err)
			}
			known := map[string]bool{}
			for _, it := range items {
				known[it.Label] = true
			}
			for _, l := range cited {
				if !known[l] {
					return false, "cites unknown label " + l
				}
			}
		}
		return true, ""
	case "relationship_count":
		idea, err := r.resolveIdea(ctx, st, a)
		if err != nil {
			return fail(err)
		}
		rels, err := store.ListRelationships(ctx, postgres.RelationshipFilter{UserID: st.user, IdeaID: &idea.ID, RelTypes: []domain.RelType{domain.RelType(a.RelType)}})
		if err != nil {
			return fail(err)
		}
		return compare(len(rels), a)
	case "focus":
		if a.Idea != "" || a.IdeaTitle != "" {
			idea, err := r.resolveIdea(ctx, st, a)
			if err != nil {
				return fail(err)
			}
			if res.Focus.IdeaID == nil || *res.Focus.IdeaID != idea.ID {
				return false, fmt.Sprintf("focus idea %v, want %s", res.Focus.IdeaID, idea.Title)
			}
		}
		if a.Checkpoint != "" {
			want := st.checkpoints[a.Checkpoint]
			if res.Focus.CheckpointID == nil || *res.Focus.CheckpointID != want {
				return false, "focus checkpoint mismatch"
			}
		}
		if a.Branch != "" {
			want := st.branches[a.Branch]
			if res.Focus.BranchID == nil || *res.Focus.BranchID != want {
				return false, "focus branch mismatch"
			}
		}
		return true, ""
	case "pending_confirmation":
		pend, err := store.PendingConfirmations(ctx, st.user)
		if err != nil {
			return fail(err)
		}
		for _, p := range pend {
			if p.ToolName == a.Tool {
				return true, ""
			}
		}
		return false, "no pending confirmation for " + a.Tool
	case "tool_status":
		for _, t := range out.Tools {
			if t.Name == a.Tool {
				return t.Status == a.Status, "status " + t.Status
			}
		}
		return false, a.Tool + " was not requested"
	case "tool_arg":
		for _, t := range out.Tools {
			if t.Name != a.Tool {
				continue
			}
			var args map[string]any
			_ = json.Unmarshal([]byte(t.Args), &args)
			got := fmt.Sprint(args[a.Arg])
			return strings.EqualFold(got, a.Value), fmt.Sprintf("%s=%q", a.Arg, got)
		}
		return false, a.Tool + " was not requested"
	}
	return false, "unknown assertion type " + a.Type
}

// Categories lists every category used by the suite (sorted).
func (s *Suite) Categories() []string {
	set := map[string]bool{}
	for _, c := range s.Cases {
		for _, cat := range c.Categories {
			set[cat] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Summary renders a one-line-per-case text summary.
func (r *Report) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent eval (%s, model %s): %d/%d passed (%.0f%%)\n", r.Mode, r.Model, r.Passed, r.Total, r.PassRate*100)
	for _, c := range r.Cases {
		mark := "PASS"
		if !c.Passed {
			mark = "FAIL"
		}
		var tools []string
		for _, t := range c.Tools {
			tools = append(tools, t.Name)
		}
		fmt.Fprintf(&b, "  %s %-45s tools=%v %dms\n", mark, c.ID, tools, c.LatencyMS)
		if c.Error != "" {
			fmt.Fprintf(&b, "       error: %s\n", c.Error)
		}
		for _, ch := range c.Checks {
			if !ch.Passed {
				fmt.Fprintf(&b, "       ✗ %s: %s\n", ch.Name, ch.Detail)
			}
		}
	}
	return b.String()
}
