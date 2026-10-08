import { describe, expect, it } from "vitest";
import {
  MAX_UPLOAD_BYTES,
  adapterLabel,
  buildCommitPayload,
  checkConversationLink,
  defaultChoices,
  importProgress,
  importSourceLabel,
  selectedCount,
  validateImportFile,
} from "@/features/imports/import-logic";
import type { ImportItem } from "@/lib/types";

function item(id: string, position: number, status = "PENDING", title = `Conversation ${id}`): ImportItem {
  return {
    id,
    import_id: "imp",
    position,
    external_id: id,
    title,
    status,
    message_count: 4,
    content_hash: "h",
    injection_flags: [],
    extracted_count: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("validateImportFile", () => {
  it("accepts supported exports", () => {
    for (const name of ["conversations.json", "export.zip", "notes.md", "t.TXT", "share.html"]) {
      expect(validateImportFile({ name, size: 10 })).toBeNull();
    }
  });
  it("rejects other types, empty and oversized files", () => {
    expect(validateImportFile({ name: "photo.png", size: 10 })).toMatch(/“\.png” files aren't supported/);
    expect(validateImportFile({ name: "README", size: 10 })).toMatch(/no extension/);
    expect(validateImportFile({ name: "a.json", size: 0 })).toBe("This file is empty.");
    expect(validateImportFile({ name: "a.zip", size: MAX_UPLOAD_BYTES + 1 })).toMatch(/limit is 200 MB/);
  });
});

describe("checkConversationLink", () => {
  it("recognises public share links", () => {
    expect(checkConversationLink("https://chatgpt.com/share/abc-123")).toMatchObject({ state: "public", provider: "ChatGPT" });
    expect(checkConversationLink("claude.ai/share/xyz")).toMatchObject({ state: "public", provider: "Claude" });
    expect(checkConversationLink("https://g.co/gemini/share/abc")).toMatchObject({ state: "public", provider: "Gemini" });
  });
  it("explains private links that need a login", () => {
    const r = checkConversationLink("https://chatgpt.com/c/0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b");
    expect(r.state).toBe("private");
    if (r.state === "private") expect(r.message).toMatch(/Share → Copy link/);
    expect(checkConversationLink("https://claude.ai/chat/123").state).toBe("private");
  });
  it("handles empty, invalid and unknown input", () => {
    expect(checkConversationLink("  ").state).toBe("empty");
    expect(checkConversationLink("ftp://x.y/z").state).toBe("invalid");
    expect(checkConversationLink("https://example.com/post").state).toBe("unknown");
  });
});

describe("commit payload", () => {
  const items = [item("b", 1), item("a", 0), item("dup", 2, "DUPLICATE"), item("bad", 3, "FAILED")];

  it("defaults: first conversation → new idea, others stored, duplicates and failures excluded", () => {
    const c = defaultChoices(items);
    expect(c.a).toEqual({ include: true, dest: { mode: "new", title: "Conversation a" } });
    expect(c.b).toEqual({ include: true, dest: { mode: "store" } });
    expect(c.dup.include).toBe(false);
    expect(c.bad.include).toBe(false);
    expect(selectedCount(items, c)).toBe(2);
  });

  it("defaults everything to the import's target idea when there is one", () => {
    const c = defaultChoices(items, "idea-1");
    expect(c.a.dest).toEqual({ mode: "idea", ideaId: "idea-1" });
    expect(c.b.dest).toEqual({ mode: "idea", ideaId: "idea-1" });
  });

  it("builds the request: skips failed items, sends custom idea titles, flags explicit duplicates", () => {
    const c = defaultChoices(items);
    c.a = { include: true, dest: { mode: "new", title: "  Clinic scheduling " } };
    c.b = { include: true, dest: { mode: "idea", ideaId: "idea-9" } };
    let p = buildCommitPayload(items, c, true);
    expect(p).toEqual({
      extract: true,
      items: [
        { item_id: "a", include: true, new_idea: true, new_idea_title: "Clinic scheduling" },
        { item_id: "b", include: true, idea_id: "idea-9" },
        { item_id: "dup", include: false },
      ],
    });
    c.dup = { include: true, dest: { mode: "store" } };
    c.a = { include: true, dest: { mode: "new", title: "Conversation a" } };
    p = buildCommitPayload(items, c, false);
    expect(p.include_duplicates).toBe(true);
    expect(p.extract).toBe(false);
    expect(p.items[0]).toEqual({ item_id: "a", include: true, new_idea: true });
    expect(p.items[2]).toEqual({ item_id: "dup", include: true });
  });
});

describe("import display helpers", () => {
  it("computes commit progress and labels", () => {
    const base = { status: "PROCESSING", stage: "commit", processed_items: 1, failed_items: 1, total_items: 4 };
    expect(importProgress(base)).toBe(0.5);
    expect(importProgress({ ...base, stage: "parse" })).toBeNull();
    expect(importProgress({ ...base, status: "COMPLETED" })).toBe(1);
    expect(importSourceLabel({ source_kind: "url", filename: "", uri: "https://chatgpt.com/share/abc" })).toBe("chatgpt.com/share/abc");
    expect(importSourceLabel({ source_kind: "paste", filename: "pasted.md", uri: "" })).toBe("Pasted conversation");
    expect(adapterLabel("chatgpt/v2")).toBe("ChatGPT · v2");
  });
});
