import { describe, expect, it } from "vitest";
import {
  analyzerLabel,
  artifactTypeName,
  generatorInfo,
  groupProvenance,
  linkifyLabels,
  markdownPreview,
  stripLeadingTitle,
} from "@/features/artifacts/artifact-meta";
import type { ProvenanceEntry } from "@/lib/types";

const id = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`;

describe("artifact names and generators", () => {
  it("humanises types", () => {
    expect(artifactTypeName("ACTION_PLAN")).toBe("Action plan");
    expect(artifactTypeName("TECHNICAL_SPEC")).toBe("Technical spec");
  });
  it("interprets generator strings", () => {
    expect(generatorInfo("template:v1")).toMatchObject({ kind: "template", label: "Template" });
    expect(generatorInfo("llm:groq/llama-3.3-70b")).toMatchObject({ kind: "ai", label: "AI · groq/llama-3.3-70b" });
    expect(generatorInfo("user")).toMatchObject({ kind: "user", label: "Manual edit" });
    expect(analyzerLabel("heuristic:v1")).toBe("Heuristic analysis");
    expect(analyzerLabel("llm:openai/gpt-4o")).toBe("AI · openai/gpt-4o");
  });
});

describe("groupProvenance", () => {
  const entries: ProvenanceEntry[] = [
    { entity_type: "decision", entity_id: id(3), label: "D3", role: "cited", title: "Build a marketplace", status: "ACTIVE" },
    { entity_type: "decision", entity_id: id(1), label: "D1", role: "cited", title: "Focus v1", status: "ACTIVE" },
    { entity_type: "decision", entity_id: id(2), label: "D2", role: "input", title: "No marketplace", status: "REVERSED" },
    { entity_type: "insight", entity_id: id(4), label: "I1", role: "input", title: "Safety matters", status: "ACTIVE" },
    { entity_type: "checkpoint", entity_id: id(5), label: "CP3", role: "base_checkpoint", title: "Research" },
    { entity_type: "idea", entity_id: id(6), label: "Biker Community Platform", role: "input", title: "Biker Community Platform" },
  ];

  it("splits roles, sorts labels and counts recorded items", () => {
    const g = groupProvenance(entries);
    expect(g.cited.map((e) => e.label)).toEqual(["D1", "D3"]);
    expect(g.inputs.map((e) => e.label)).toEqual(["D2", "I1"]);
    expect(g.baseCheckpoint.map((e) => e.label)).toEqual(["CP3"]);
    expect(g.idea?.title).toBe("Biker Community Platform");
    expect(g.itemCount).toBe(4);
    expect(g.dead.map((e) => e.label)).toEqual(["D2"]);
  });

  it("tolerates empty input", () => {
    expect(groupProvenance(undefined)).toMatchObject({ cited: [], inputs: [], itemCount: 0, dead: [] });
  });
});

describe("markdown helpers", () => {
  it("builds a plain preview without the title heading or syntax", () => {
    const md = "# Plan\n\n_Action Plan for **Biker** · branch Main._\n\n## Goal\n\n- [ ] Ship the [trip planner](https://x.y) `now` [D1]";
    expect(markdownPreview(md)).toBe("Ship the trip planner now [D1]");
    expect(markdownPreview("Intro with _emphasis_ here.\n\n- [x] done")).toBe("Intro with emphasis here. done");
    expect(markdownPreview("word ".repeat(100), 20)).toHaveLength(20);
    expect(markdownPreview("Status: **DECIDED**.\n\n#…")).toBe("Status: DECIDED.");
  });

  it("strips a leading H1 only when it repeats the title", () => {
    expect(stripLeadingTitle("# My Plan\n\nBody", "My plan")).toBe("Body");
    expect(stripLeadingTitle("# Other\n\nBody", "My plan")).toBe("# Other\n\nBody");
    expect(stripLeadingTitle("Body", "My plan")).toBe("Body");
  });

  it("turns known [labels] into iv:// links and leaves others alone", () => {
    const out = linkifyLabels("See [D1] and [Q9], not [D1](https://x) or [link][D1].", [{ label: "D1", type: "decision", id: id(1) }]);
    expect(out).toBe(`See [D1](iv://decision/${id(1)}) and [Q9], not [D1](https://x) or [link][D1](iv://decision/${id(1)}).`);
  });
});
