package models

import (
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

func cfgModel(provider, model string, speed, quality, cost, ctxLen int, tools, json bool) domain.ModelConfig {
	return domain.ModelConfig{Provider: provider, Model: model, Speed: speed, Quality: quality, RelativeCost: cost, ContextLength: ctxLen,
		ToolCalling: tools, StructuredOutput: json, Enabled: true}
}

var testCatalog = []domain.ModelConfig{
	cfgModel("groq", "fast-small", 5, 2, 0, 131072, true, true),     // fast & cheap
	cfgModel("openai", "big-strong", 3, 5, 2, 400000, true, true),   // slower, pricier, best quality
	cfgModel("gemini", "balanced", 4, 4, 1, 1048576, true, false),   // balanced, no native JSON
	cfgModel("ollama", "tiny-local", 2, 2, 0, 8192, false, false),   // small context, no tools
	cfgModel(ProviderMock, MockModel, 5, 1, 0, 1000000, true, true), // offline planner
}

func allAvailable(string) bool { return true }

func keys(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Key()
	}
	return out
}

func TestRouteTiers(t *testing.T) {
	fast := Route(TaskTitle, testCatalog, allAvailable, RoutingPolicy{})
	if len(fast) == 0 || fast[0].Key() != "groq/fast-small" {
		t.Fatalf("fast tier should prefer the fast, cheap model; got %v", keys(fast))
	}
	strong := Route(TaskSynthesis, testCatalog, allAvailable, RoutingPolicy{})
	if len(strong) == 0 || strong[0].Key() != "openai/big-strong" {
		t.Fatalf("strong tier should prefer quality; got %v", keys(strong))
	}
	for i := 1; i < len(strong); i++ {
		if strong[i-1].Score < strong[i].Score {
			t.Errorf("candidates not sorted by score: %v", keys(strong))
		}
	}
	for _, c := range append(fast, strong...) {
		if c.Reason == "" {
			t.Errorf("%s has no routing reason", c.Key())
		}
	}
}

func TestRouteCapabilityFiltering(t *testing.T) {
	sup := Route(TaskSupervisor, testCatalog, allAvailable, RoutingPolicy{})
	for _, c := range sup {
		if !c.Config.ToolCalling {
			t.Errorf("supervisor routed to %s without tool calling", c.Key())
		}
		if c.Config.ContextLength < 32000 {
			t.Errorf("supervisor routed to %s with only %d context", c.Key(), c.Config.ContextLength)
		}
	}
	for _, c := range Route(TaskSynthesis, testCatalog, allAvailable, RoutingPolicy{}) {
		if c.Key() == "ollama/tiny-local" {
			t.Error("tasks with MinContext must exclude small-context models")
		}
	}
	// JSON tasks give a bonus to native structured output.
	ex := Route(TaskExtraction, []domain.ModelConfig{
		cfgModel("a", "json", 4, 3, 1, 100000, false, true),
		cfgModel("b", "nojson", 4, 3, 1, 100000, false, false),
	}, allAvailable, RoutingPolicy{})
	if ex[0].Key() != "a/json" || ex[0].Score != ex[1].Score+1 {
		t.Errorf("structured-output bonus missing: %v (%v vs %v)", keys(ex), ex[0].Score, ex[1].Score)
	}
	// Disabled models and unconfigured providers are never routed to.
	disabled := cfgModel("openai", "off", 5, 5, 0, 400000, true, true)
	disabled.Enabled = false
	got := Route(TaskSynthesis, append([]domain.ModelConfig{disabled}, testCatalog...), func(p string) bool { return p != "openai" }, RoutingPolicy{})
	for _, c := range got {
		if c.Config.Provider == "openai" {
			t.Errorf("routed to unavailable/disabled %s", c.Key())
		}
	}
}

func TestRouteMockOnlyAsLastResort(t *testing.T) {
	got := Route(TaskSupervisor, testCatalog, allAvailable, RoutingPolicy{})
	for _, c := range got {
		if c.Config.Provider == ProviderMock {
			t.Fatalf("mock planner must not be offered while real models qualify: %v", keys(got))
		}
	}
	onlyMock := Route(TaskSupervisor, testCatalog, func(p string) bool { return p == ProviderMock }, RoutingPolicy{})
	if len(onlyMock) != 1 || onlyMock[0].Config.Provider != ProviderMock {
		t.Fatalf("with no real provider the offline planner is the fallback, got %v", keys(onlyMock))
	}
	if onlyMock[0].Reason == "" {
		t.Error("fallback should explain itself")
	}
	none := Route(TaskSupervisor, testCatalog, func(string) bool { return false }, RoutingPolicy{})
	if len(none) != 0 {
		t.Errorf("nothing available → no candidates, got %v", keys(none))
	}
}

func TestRoutePinsAndPreferences(t *testing.T) {
	pinned := Route(TaskSynthesis, testCatalog, allAvailable, RoutingPolicy{Pins: map[Task]string{TaskSynthesis: "gemini/balanced"}})
	if pinned[0].Key() != "gemini/balanced" {
		t.Fatalf("pin must win: %v", keys(pinned))
	}
	// A pin applies only to its task.
	other := Route(TaskSummary, testCatalog, allAvailable, RoutingPolicy{Pins: map[Task]string{TaskSynthesis: "gemini/balanced"}})
	if other[0].Key() == "gemini/balanced" && other[0].Score >= 1000 {
		t.Error("pin leaked into another task")
	}
	// Pinning the offline planner puts it first even when real models exist.
	mp := Route(TaskSupervisor, testCatalog, allAvailable, RoutingPolicy{Pins: map[Task]string{TaskSupervisor: ProviderMock + "/" + MockModel}})
	if mp[0].Config.Provider != ProviderMock {
		t.Fatalf("pinned offline planner should be first: %v", keys(mp))
	}
	pref := Route(TaskSynthesis, testCatalog, allAvailable, RoutingPolicy{PreferProviders: []string{"groq"}})
	base := Route(TaskSynthesis, testCatalog, allAvailable, RoutingPolicy{})
	score := func(cs []Candidate, key string) float64 {
		for _, c := range cs {
			if c.Key() == key {
				return c.Score
			}
		}
		t.Fatalf("%s missing", key)
		return 0
	}
	if score(pref, "groq/fast-small") <= score(base, "groq/fast-small") {
		t.Error("preferred providers must be boosted")
	}
	capped := Route(TaskSynthesis, testCatalog, allAvailable, RoutingPolicy{MaxRelativeCost: 1})
	for _, c := range capped {
		if c.Config.RelativeCost > 1 {
			t.Errorf("cost cap ignored for %s", c.Key())
		}
	}
	cappedPinned := Route(TaskSynthesis, testCatalog, allAvailable, RoutingPolicy{MaxRelativeCost: 1, Pins: map[Task]string{TaskSynthesis: "openai/big-strong"}})
	if cappedPinned[0].Key() != "openai/big-strong" {
		t.Error("an explicit pin overrides the cost cap")
	}
}

func TestSpecForUnknownTask(t *testing.T) {
	s := SpecFor("brand_new_task")
	if s.Tier != TierStrong || s.Task != "brand_new_task" {
		t.Errorf("unknown tasks default to strong: %+v", s)
	}
	for task := range TaskSpecs {
		if SpecFor(task).Task != task {
			t.Errorf("SpecFor(%s) lost the task id", task)
		}
	}
}

func TestBuiltinCatalogSane(t *testing.T) {
	seen := map[string]bool{}
	mock := 0
	for _, m := range BuiltinModels {
		k := m.Provider + "/" + m.Model
		if seen[k] {
			t.Errorf("duplicate catalog entry %s", k)
		}
		seen[k] = true
		if m.Speed < 1 || m.Speed > 5 || m.Quality < 1 || m.Quality > 5 || m.RelativeCost < 0 || m.RelativeCost > 5 {
			t.Errorf("%s has out-of-range scores (DB check constraints would reject it)", k)
		}
		if m.Provider == ProviderMock {
			mock++
		}
	}
	if mock != 1 {
		t.Errorf("catalog must contain exactly one offline planner, has %d", mock)
	}
}
