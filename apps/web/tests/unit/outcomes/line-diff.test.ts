import { describe, expect, it } from "vitest";
import { collapseUnchanged, diffLines, diffStats, splitLines } from "@/features/artifacts/line-diff";

const ops = (a: string, b: string) => diffLines(a, b).map((l) => `${l.op === "equal" ? " " : l.op === "add" ? "+" : "-"}${l.text}`);

describe("splitLines", () => {
  it("normalises newlines and ignores a single trailing newline", () => {
    expect(splitLines("")).toEqual([]);
    expect(splitLines("a\r\nb\n")).toEqual(["a", "b"]);
    expect(splitLines("a\n\n")).toEqual(["a", ""]);
  });
});

describe("diffLines", () => {
  it("returns only equal lines for identical input", () => {
    const d = diffLines("one\ntwo", "one\ntwo");
    expect(d.every((l) => l.op === "equal")).toBe(true);
    expect(d.map((l) => [l.a, l.b])).toEqual([
      [1, 1],
      [2, 2],
    ]);
  });

  it("finds insertions, deletions and replacements via LCS", () => {
    expect(ops("a\nb\nc\nd", "a\nc\nd\ne")).toEqual([" a", "-b", " c", " d", "+e"]);
    expect(ops("# Plan\n- step one\n- step two", "# Plan\n- step one (revised)\n- step two")).toEqual([
      " # Plan",
      "-- step one",
      "+- step one (revised)",
      " - step two",
    ]);
  });

  it("handles empty sides", () => {
    expect(ops("", "x\ny")).toEqual(["+x", "+y"]);
    expect(ops("x", "")).toEqual(["-x"]);
  });

  it("keeps correct line numbers on both sides", () => {
    const d = diffLines("a\nb\nc", "z\na\nc");
    expect(d).toEqual([
      { op: "add", text: "z", b: 1 },
      { op: "equal", text: "a", a: 1, b: 2 },
      { op: "del", text: "b", a: 2 },
      { op: "equal", text: "c", a: 3, b: 3 },
    ]);
  });

  it("produces a minimal edit for moved blocks", () => {
    const s = diffStats(diffLines("1\n2\n3\n4\n5", "1\n3\n4\n5\n2"));
    expect(s).toEqual({ added: 1, removed: 1, unchanged: 4 });
  });
});

describe("collapseUnchanged", () => {
  it("folds long unchanged runs but keeps context around changes", () => {
    const a = Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join("\n");
    const b = a.replace("line 10", "line ten");
    const chunks = collapseUnchanged(diffLines(a, b), 2);
    expect(chunks.map((c) => [c.kind, c.lines.length])).toEqual([
      ["skip", 7],
      ["lines", 6],
      ["skip", 8],
    ]);
  });

  it("does not fold tiny runs", () => {
    const chunks = collapseUnchanged(diffLines("a\nb\nc", "a\nB\nc"), 0);
    expect(chunks).toHaveLength(1);
    expect(chunks[0].kind).toBe("lines");
  });
});
