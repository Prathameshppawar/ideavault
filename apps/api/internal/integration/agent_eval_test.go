package integration

import (
	"path/filepath"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/evals/agenteval"
)

// TestAgentEvaluationCases runs every case in tests/evaluations/agent/cases.json
// against the deterministic offline planner (strict tool-prefix matching) and writes
// tests/evaluations/reports/agent-offline.json.
func TestAgentEvaluationCases(t *testing.T) {
	e := requireEnv(t)
	path, err := agenteval.DefaultSuitePath()
	if err != nil {
		t.Fatal(err)
	}
	suite, err := agenteval.LoadSuite(path)
	if err != nil {
		t.Fatalf("load cases: %v", err)
	}
	required := []string{"idea_identification", "correct_branch", "correct_checkpoint", "avoid_irrelevant_history", "contradiction_detection",
		"correct_tools", "permission_respect", "injection_resistance", "provenance", "structured_knowledge", "grounded_artifact"}
	have := map[string]bool{}
	for _, c := range suite.Categories() {
		have[c] = true
	}
	for _, c := range required {
		if !have[c] {
			t.Errorf("no evaluation case covers category %q", c)
		}
	}
	r := &agenteval.Runner{App: e.App, Strict: true}
	rep := r.Run(ctx(), suite)
	rep.Model = "mock/offline-planner"
	root, err := agenteval.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "tests", "evaluations", "reports", "agent-offline.json")
	if err := agenteval.WriteReport(out, rep); err != nil {
		t.Errorf("write report: %v", err)
	}
	for _, c := range rep.Cases {
		if !c.Passed {
			t.Errorf("case %s failed (status %s, tools %v): error=%q", c.ID, c.Status, c.Tools, c.Error)
			for _, ch := range c.Checks {
				if !ch.Passed {
					t.Errorf("    %s: %s", ch.Name, ch.Detail)
				}
			}
		}
	}
	if rep.Total != len(suite.Cases) || rep.Total < 15 {
		t.Errorf("ran %d of %d cases", rep.Total, len(suite.Cases))
	}
	t.Logf("%s(report: %s)", rep.Summary(), out)
}

// The runner must actually catch regressions: a case with wrong expectations fails
// with precise check details.
func TestAgentEvalRunnerDetectsFailures(t *testing.T) {
	e := requireEnv(t)
	path, _ := agenteval.DefaultSuitePath()
	suite, err := agenteval.LoadSuite(path)
	if err != nil {
		t.Fatal(err)
	}
	var c agenteval.Case
	for _, x := range suite.Cases {
		if x.ID == "destructive-requires-confirmation" {
			c = x
		}
	}
	c.Expect.Status = "COMPLETED"
	c.Expect.ForbiddenTools = []string{"delete_idea"}
	c.Expect.ToolPrefix = []string{"search_vault"}
	res := (&agenteval.Runner{App: e.App, Strict: true}).RunCase(ctx(), c)
	if res.Passed {
		t.Fatal("a case with wrong expectations must fail")
	}
	failed := map[string]bool{}
	for _, ch := range res.Checks {
		if !ch.Passed {
			failed[ch.Name] = true
			if ch.Detail == "" {
				t.Errorf("failing check %s has no detail", ch.Name)
			}
		}
	}
	for _, want := range []string{"status", "forbidden_tools", "tool_sequence"} {
		if !failed[want] {
			t.Errorf("check %s should fail: %+v", want, res.Checks)
		}
	}
	// Lenient mode (real models) accepts extra steps but keeps the order.
	lenient := c
	lenient.Expect = agenteval.Expect{Status: "AWAITING_CONFIRMATION", ToolPrefix: []string{"delete_idea"}}
	if res := (&agenteval.Runner{App: e.App}).RunCase(ctx(), lenient); !res.Passed {
		t.Errorf("lenient run failed: %+v", res.Checks)
	}
}
