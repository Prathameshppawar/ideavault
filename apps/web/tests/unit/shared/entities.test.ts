import { describe, expect, it } from "vitest";
import { entityHref, parseIvLink } from "@/lib/entities";
import { humanize, plural } from "@/lib/format";
import { qs } from "@/lib/api";

describe("entities", () => {
  it("parses iv:// links and maps them to routes", () => {
    const id = "023d5bfe-045e-4021-b0f1-7ac18d7694d8";
    expect(parseIvLink(`iv://decision/${id}`)).toEqual({ type: "decision", id });
    expect(parseIvLink("https://example.com")).toBeNull();
    expect(entityHref("decision", id)).toBe(`/knowledge/${id}`);
    expect(entityHref("idea", id)).toBe(`/ideas/${id}`);
    expect(entityHref("artifact", id)).toBe(`/artifacts/${id}`);
  });
  it("formats", () => {
    expect(humanize("READY_TO_IMPLEMENT")).toBe("Ready to implement");
    expect(plural(1, "decision")).toBe("1 decision");
    expect(plural(2, "decision")).toBe("2 decisions");
    expect(qs({ a: "x", b: undefined, c: ["1", "2"], d: "" })).toBe("?a=x&c=1%2C2");
  });
});
