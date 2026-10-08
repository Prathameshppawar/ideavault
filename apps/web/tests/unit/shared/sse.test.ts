import { describe, expect, it } from "vitest";
import { createSSEParser, type SSEMessage } from "@/lib/sse";

describe("createSSEParser", () => {
  it("parses events split across chunks, skips comments and joins multi-line data", () => {
    const got: SSEMessage[] = [];
    const parse = createSSEParser((m) => got.push(m));
    parse('event: token\ndata: {"text":"Hel');
    parse('lo"}\n\n: keep-alive\n\nevent: trace\r\ndata: {"label":"Searching"}\r\n\r\n');
    parse("data: line1\ndata: line2\n\n");
    expect(got).toEqual([
      { event: "token", data: { text: "Hello" } },
      { event: "trace", data: { label: "Searching" } },
      { event: "message", data: "line1\nline2" },
    ]);
  });
});
