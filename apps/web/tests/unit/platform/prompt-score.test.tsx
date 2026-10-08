import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { OverallScore, RubricGrid } from "@/features/insights/prompt-score";
import { analyzerLabel, groupPatterns, rubric, statsSentences } from "@/features/insights/insights-data";

describe("prompt score rendering", () => {
  it("renders all 8 rubric dimensions in order with text labels (not colour alone)", () => {
    render(<RubricGrid scores={{ context: 1, goal: 2, constraints: 0, specificity: 0, examples: 0, expected_output: 0, clarity: 0, completeness: 1 }} />);
    const list = screen.getByRole("list", { name: "Prompt rubric" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(8);
    expect(items[0]).toHaveAccessibleName("Context: partial (1 of 2)");
    expect(items[1]).toHaveAccessibleName("Goal: strong (2 of 2)");
    expect(items[5]).toHaveAccessibleName("Expected output: absent (0 of 2)");
    expect(within(items[1]).getByText("Strong")).toBeInTheDocument();
  });

  it("clamps out-of-range and missing scores", () => {
    expect(rubric({ context: 7, goal: -1 }).slice(0, 3).map((r) => r.score)).toEqual([2, 0, 0]);
  });

  it("shows the overall score with its band", () => {
    render(<OverallScore overall={25} />);
    expect(screen.getByLabelText("Overall score 25 out of 100")).toHaveTextContent("25");
    expect(screen.getByText("Needs more to go on")).toBeInTheDocument();
  });

  it("labels analyzers in plain words", () => {
    expect(analyzerLabel("heuristic:v1")).toBe("transparent heuristic rules");
    expect(analyzerLabel("deterministic:v1 + llm:groq/openai/gpt-oss-120b")).toBe("deterministic graph statistics + AI model groq/openai/gpt-oss-120b");
  });
});

describe("thinking analysis helpers", () => {
  it("turns stats into readable sentences", () => {
    const s = statsSentences(
      { decisions: 5, decisions_changed: 1, decisions_with_rationale: 5, assumptions_unvalidated: 2, assumptions_stale: 0, questions_open: 3, ideas: 6, branches_per_idea: 1.2, ideas_parked_or_abandoned: 1, prompts_analyzed: 5 },
      "vault",
    );
    expect(s.join(" ")).toBe(
      "Across 6 ideas you've recorded 5 decisions — 1 later changed, and all have a written rationale. 2 assumptions are still unvalidated and 3 questions are open. Ideas average 1.2 branches; 1 is parked or abandoned. Prompt habits are based on 5 messages you wrote yourself (imported content is excluded).",
    );
    expect(statsSentences({ decisions: 0 }, "idea")[0]).toBe("In this idea you haven't recorded any accepted decisions yet.");
    expect(statsSentences(null, "vault")).toEqual([]);
  });

  it("groups patterns by kind in a fixed order and handles null", () => {
    const p = (id: string, kind: string) => ({ id, kind, title: id, description: "", metric: "", evidence: [] });
    expect(groupPatterns([p("r", "risk"), p("s", "strength"), p("x", "other"), p("h", "habit")]).map((g) => g.kind)).toEqual(["strength", "habit", "risk", "other"]);
    expect(groupPatterns(null)).toEqual([]);
  });
});
