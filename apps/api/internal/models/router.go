package models

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
)

// Task identifies the kind of work a model call performs; routing is per task.
type Task string

const (
	TaskSupervisor       Task = "supervisor"
	TaskExtraction       Task = "extraction"
	TaskClassification   Task = "classification"
	TaskSummary          Task = "summary"
	TaskTitle            Task = "title"
	TaskSynthesis        Task = "synthesis"
	TaskArtifact         Task = "artifact"
	TaskContradiction    Task = "contradiction"
	TaskBranchComparison Task = "branch_comparison"
	TaskPromptAnalysis   Task = "prompt_analysis"
	TaskThinkingAnalysis Task = "thinking_analysis"
	TaskDelta            Task = "delta"
	TaskLab              Task = "model_lab"
)

// Tier is a coarse quality/speed preference.
type Tier string

const (
	TierFast   Tier = "fast"
	TierStrong Tier = "strong"
)

// TaskSpec declares what a task requires from a model.
type TaskSpec struct {
	Task        Task   `json:"task"`
	Tier        Tier   `json:"tier"`
	NeedsTools  bool   `json:"needs_tools"`
	NeedsJSON   bool   `json:"needs_json"`
	MinContext  int    `json:"min_context"`
	Description string `json:"description"`
}

// TaskSpecs is the routing table: task → required capabilities.
var TaskSpecs = map[Task]TaskSpec{
	TaskSupervisor:       {Tier: TierStrong, NeedsTools: true, MinContext: 32000, Description: "Agent Supervisor: interprets requests and operates tools"},
	TaskExtraction:       {Tier: TierFast, NeedsJSON: true, MinContext: 16000, Description: "Extract decisions, assumptions, evidence… from conversations"},
	TaskClassification:   {Tier: TierFast, NeedsJSON: true, Description: "Classify intents and items"},
	TaskSummary:          {Tier: TierFast, Description: "Short summaries (checkpoints, conversations)"},
	TaskTitle:            {Tier: TierFast, Description: "Titles and metadata"},
	TaskSynthesis:        {Tier: TierStrong, MinContext: 32000, Description: "Strategic synthesis and deep answers"},
	TaskArtifact:         {Tier: TierStrong, MinContext: 32000, Description: "Generate Markdown artifacts (plans, specs, prompts)"},
	TaskContradiction:    {Tier: TierStrong, NeedsJSON: true, Description: "Detect contradictions and changed decisions"},
	TaskBranchComparison: {Tier: TierStrong, Description: "Compare branches and explain differences"},
	TaskPromptAnalysis:   {Tier: TierStrong, NeedsJSON: true, Description: "Evaluate prompt quality"},
	TaskThinkingAnalysis: {Tier: TierStrong, NeedsJSON: true, Description: "Identify observable thinking patterns"},
	TaskDelta:            {Tier: TierStrong, NeedsJSON: true, MinContext: 32000, Description: "Analyze external AI conversations against a branch"},
	TaskLab:              {Tier: TierStrong, Description: "Model Lab comparisons"},
}

// SpecFor returns the spec of a task (defaulting to a strong chat task).
func SpecFor(t Task) TaskSpec {
	s, ok := TaskSpecs[t]
	if !ok {
		s = TaskSpec{Tier: TierStrong}
	}
	s.Task = t
	return s
}

// Candidate is a routable model with the reason it was ranked where it is.
type Candidate struct {
	Config domain.ModelConfig `json:"config"`
	Score  float64            `json:"score"`
	Reason string             `json:"reason"`
}

// Key returns "provider/model".
func (c Candidate) Key() string { return c.Config.Provider + "/" + c.Config.Model }

// RoutingPolicy carries user preferences.
type RoutingPolicy struct {
	// Pins maps a task to a "provider/model" key the user prefers.
	Pins map[Task]string `json:"pins,omitempty"`
	// PreferProviders boosts providers in order.
	PreferProviders []string `json:"prefer_providers,omitempty"`
	// MaxRelativeCost excludes models more expensive than this (0 = no limit).
	MaxRelativeCost int `json:"max_relative_cost,omitempty"`
}

// Route ranks available models for a task. available reports whether a provider is configured.
// The mock planner is only returned when no real model qualifies (or it is pinned).
func Route(task Task, models []domain.ModelConfig, available func(provider string) bool, pol RoutingPolicy) []Candidate {
	spec := SpecFor(task)
	var real, mock []Candidate
	pin := pol.Pins[task]
	for _, m := range models {
		if !m.Enabled || !available(m.Provider) {
			continue
		}
		if spec.NeedsTools && !m.ToolCalling {
			continue
		}
		if spec.MinContext > 0 && m.ContextLength < spec.MinContext {
			continue
		}
		key := m.Provider + "/" + m.Model
		if pol.MaxRelativeCost > 0 && m.RelativeCost > pol.MaxRelativeCost && key != pin {
			continue
		}
		var score float64
		var why []string
		if spec.Tier == TierFast {
			score = float64(m.Speed)*3 - float64(m.RelativeCost)*2 + float64(m.Quality)
			why = append(why, fmt.Sprintf("fast tier: speed %d, cost %d, quality %d", m.Speed, m.RelativeCost, m.Quality))
		} else {
			score = float64(m.Quality)*3 + float64(m.Speed) - float64(m.RelativeCost)
			why = append(why, fmt.Sprintf("strong tier: quality %d, speed %d, cost %d", m.Quality, m.Speed, m.RelativeCost))
		}
		if spec.NeedsJSON && m.StructuredOutput {
			score += 1
			why = append(why, "native structured output")
		}
		for i, p := range pol.PreferProviders {
			if p == m.Provider {
				score += float64(len(pol.PreferProviders)-i) * 2
				why = append(why, "preferred provider")
			}
		}
		if key == pin {
			score += 1000
			why = append(why, "pinned by you for this task")
		}
		c := Candidate{Config: m, Score: score, Reason: strings.Join(why, "; ")}
		if m.Provider == ProviderMock {
			c.Reason = "offline fallback: no AI provider configured"
			if key == pin {
				c.Score = 1000
				c.Reason = "pinned offline planner"
				real = append(real, c)
				continue
			}
			mock = append(mock, c)
			continue
		}
		real = append(real, c)
	}
	sort.SliceStable(real, func(i, j int) bool { return real[i].Score > real[j].Score })
	if len(real) > 0 {
		return real
	}
	return mock
}
