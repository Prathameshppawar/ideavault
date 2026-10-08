import { describe, expect, it } from "vitest";
import { buildRoutingPolicy, draftFromSettings, move, samePolicy } from "@/features/models/routing";
import { decide, toggleDisabled, describeUserAgent } from "@/features/settings/policy";

describe("routing pin payload builder", () => {
  it("reads the stored policy from settings and tolerates junk", () => {
    expect(draftFromSettings(undefined)).toEqual({ pins: {}, prefer: [], maxCost: 0 });
    expect(draftFromSettings({ routing: "nope" })).toEqual({ pins: {}, prefer: [], maxCost: 0 });
    expect(
      draftFromSettings({ routing: { pins: { supervisor: "groq/openai/gpt-oss-120b", title: "", bad: 3 }, prefer_providers: ["groq", 7, ""], max_relative_cost: 2.4 } }),
    ).toEqual({ pins: { supervisor: "groq/openai/gpt-oss-120b" }, prefer: ["groq"], maxCost: 2 });
  });

  it("builds the full PUT body: drops automatic pins, dedupes providers, clamps cost", () => {
    const body = buildRoutingPolicy({
      pins: { supervisor: " anthropic/claude-sonnet-5-5 ", extraction: "", title: "groq/openai/gpt-oss-20b" },
      prefer: ["Groq", "anthropic", "groq", " "],
      maxCost: 9,
    });
    expect(body).toEqual({
      pins: { supervisor: "anthropic/claude-sonnet-5-5", title: "groq/openai/gpt-oss-20b" },
      prefer_providers: ["groq", "anthropic"],
      max_relative_cost: 5,
    });
    expect(buildRoutingPolicy({ pins: {}, prefer: [], maxCost: 0 })).toEqual({ pins: {}, prefer_providers: [], max_relative_cost: 0 });
  });

  it("detects real changes only", () => {
    const a = { pins: { title: "x/y", summary: "" }, prefer: ["groq"], maxCost: 0 };
    const b = { pins: { title: "x/y" }, prefer: ["groq"], maxCost: 0 };
    expect(samePolicy(a, b)).toBe(true);
    expect(samePolicy(a, { ...b, prefer: ["openai"] })).toBe(false);
    expect(samePolicy(a, { ...b, pins: { title: "x/z" } })).toBe(false);
  });

  it("reorders preferred providers", () => {
    expect(move(["a", "b", "c"], 2, -1)).toEqual(["a", "c", "b"]);
    expect(move(["a", "b"], 0, -1)).toEqual(["a", "b"]);
  });
});

describe("agent permission policy mirror", () => {
  const base = { confirm_writes: false, auto_external: false, disabled_tools: [] as string[] };
  it("matches the server rules", () => {
    expect(decide("search_vault", "READ", base)).toBe("allow");
    expect(decide("record_decision", "WRITE", base)).toBe("allow");
    expect(decide("record_decision", "WRITE", { ...base, confirm_writes: true })).toBe("confirm");
    expect(decide("web_fetch", "EXTERNAL", base)).toBe("confirm");
    expect(decide("web_fetch", "EXTERNAL", { ...base, auto_external: true })).toBe("allow");
    // Destructive actions can never be auto-approved.
    expect(decide("delete_idea", "DESTRUCTIVE", { confirm_writes: false, auto_external: true })).toBe("confirm");
    expect(decide("search_vault", "READ", toggleDisabled(base, "search_vault", true))).toBe("deny");
  });
  it("toggles disabled tools without duplicates", () => {
    const p = toggleDisabled(toggleDisabled(base, "web_fetch", true), "web_fetch", true);
    expect(p.disabled_tools).toEqual(["web_fetch"]);
    expect(toggleDisabled(p, "web_fetch", false).disabled_tools).toEqual([]);
  });
  it("describes sessions by device", () => {
    expect(describeUserAgent("curl/8.7.1")).toBe("curl");
    expect(describeUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36")).toBe("Chrome on macOS");
  });
});
