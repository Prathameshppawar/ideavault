import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { cleanSnippet, parseHighlights } from "@/features/search/highlight";
import { describeInterpretation, listJoin } from "@/features/search/interpretation";
import { Snippet, sameText } from "@/features/search/search-screen";

describe("parseHighlights", () => {
  it("splits «marked» terms into highlighted segments", () => {
    expect(parseHighlights("«Biker» community for «bikers»!")).toEqual([
      { text: "Biker", hit: true },
      { text: " community for ", hit: false },
      { text: "bikers", hit: true },
      { text: "!", hit: false },
    ]);
  });
  it("handles no markers, adjacent marks and stray markers", () => {
    expect(parseHighlights("plain")).toEqual([{ text: "plain", hit: false }]);
    expect(parseHighlights("«AI»«Automation»")).toEqual([{ text: "AIAutomation", hit: true }]);
    expect(parseHighlights("stray » close and « open")).toEqual([{ text: "stray  close and  open", hit: false }]);
    expect(parseHighlights("«nested «x» ok»")).toEqual([
      { text: "nested ", hit: false },
      { text: "x", hit: true },
      { text: " ok", hit: false },
    ]);
    expect(parseHighlights("")).toEqual([]);
  });
});

describe("cleanSnippet", () => {
  it("strips markdown syntax, code fences and box art but keeps match markers", () => {
    expect(cleanSnippet("_Action Plan for **«Biker» Community**_ · branch Main")).toBe("Action Plan for «Biker» Community · branch Main");
    expect(cleanSnippet("```text\n┌──────┐\n│ Attachments │")).toBe("Attachments");
    expect(cleanSnippet("## Next steps\n- see [the guide](https://x.y) now")).toBe("Next steps see the guide now");
    expect(cleanSnippet("snake_case_name stays")).toBe("snake_case_name stays");
  });
});

describe("Snippet", () => {
  it("renders matches as <mark> and never injects HTML", () => {
    const { container } = render(<Snippet text={'<img src=x onerror="alert(1)"> found «decision» here'} />);
    expect(container.querySelector("img")).toBeNull();
    const marks = container.querySelectorAll("mark");
    expect(marks).toHaveLength(1);
    expect(marks[0].textContent).toBe("decision");
    expect(container.textContent).toContain('<img src=x onerror="alert(1)">');
  });
  it("detects snippets that merely repeat the title", () => {
    expect(sameText("«Riders» already coordinate trips", "Riders already coordinate trips")).toBe(true);
    expect(sameText("…a community platform for «bikers»", "Biker Community Platform")).toBe(false);
  });
});

describe("describeInterpretation", () => {
  it("builds a plain sentence from the structured interpretation", () => {
    expect(listJoin(["a", "b", "c"])).toBe("a, b and c");
    expect(describeInterpretation({ terms: "ai automation", types: ["message", "conversation", "idea"], order: "oldest", explanation: "" })).toBe(
      "Showing messages, conversations and ideas matching “ai automation”, earliest first.",
    );
    expect(describeInterpretation({ terms: "", types: ["assumption"], statuses: ["UNVALIDATED", "VALIDATING"], order: "relevance", explanation: "" })).toBe(
      "Showing assumptions that haven't been validated, most relevant first.",
    );
    expect(describeInterpretation({ terms: "HireHub", types: ["idea"], similar_to: "HireHub", order: "relevance", explanation: "" })).toBe(
      "Showing ideas similar to “HireHub”, most relevant first.",
    );
    expect(describeInterpretation({ terms: "biker", order: "newest", explanation: "" })).toBe("Showing everything in your vault matching “biker”, newest first.");
  });
});
