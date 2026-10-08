import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Idea } from "@/lib/types";
import { ConcludeDialog } from "@/features/ideas/conclude-dialog";
import { KnowledgeSections } from "@/features/knowledge/knowledge-sections";
import { item } from "./fixtures";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const idea = (status: string): Idea =>
  ({
    id: "idea-1",
    title: "Biker Community Platform",
    slug: "biker",
    origin_text: "",
    summary: "",
    status,
    outcome_note: "",
    tags: [],
    version: 1,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    last_activity_at: "2026-10-01T00:00:00Z",
  }) as Idea;

describe("ConcludeDialog", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    fetchMock = vi.fn(async (url: string) => {
      if (String(url).startsWith("/api/v1/ideas?")) {
        return new Response(JSON.stringify({ ideas: [idea("ACTIVE"), { ...idea("ACTIVE"), id: "idea-2", title: "Rider safety app" }], total: 2 }), {
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ artifacts: [], conclusion: null }), { headers: { "content-type": "application/json" } });
    });
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("disables outcomes a parked idea can't reach and explains why", () => {
    wrap(<ConcludeDialog open onOpenChange={() => {}} idea={idea("PARKED")} branchId="b" branchName="Main" items={[]} />);
    expect(screen.getByRole("radio", { name: /Implement/ })).toBeDisabled();
    expect(screen.getByRole("radio", { name: /Abandon/ })).toBeEnabled();
    expect(screen.getByRole("radio", { name: /Merge/ })).toBeEnabled();
    expect(screen.getByText(/Concluding never deletes anything/)).toBeInTheDocument();
  });

  it("preselects the suggested outcome with its artifacts and requires a merge target", async () => {
    const user = userEvent.setup();
    const items = [1, 2, 3].map((n) => item({ kind: "decision", ref_number: n }));
    wrap(<ConcludeDialog open onOpenChange={() => {}} idea={idea("ACTIVE")} branchId="b" branchName="Main" items={items} />);
    expect(screen.getByRole("radio", { name: /Implement/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("checkbox", { name: /Implementation Prompt/ })).toBeChecked();

    await user.click(screen.getByRole("radio", { name: /Merge/ }));
    await user.click(screen.getByRole("button", { name: /Conclude as merge/ }));
    expect(await screen.findByText("Pick the idea to merge into.")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/conclude"))).toBe(false);

    // The idea itself is never offered as a merge target.
    const select = await screen.findByLabelText("Merge into");
    await waitFor(() => expect(screen.getByRole("option", { name: /Rider safety app/ })).toBeInTheDocument());
    expect(screen.queryByRole("option", { name: /Biker Community Platform/ })).toBeNull();
    await user.selectOptions(select, "idea-2");
    await user.click(screen.getByRole("button", { name: /Conclude as merge/ }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/v1/ideas/idea-1/conclude"))).toBe(true));
    const call = fetchMock.mock.calls.find(([u]) => String(u).includes("/conclude"))!;
    expect(JSON.parse(String((call[1] as RequestInit).body))).toMatchObject({ outcome: "MERGE", merge_into_idea_id: "idea-2", branch_id: "b" });
  });
});

describe("KnowledgeSections", () => {
  const d2 = item({ kind: "decision", ref_number: 2, statement: "No marketplace in v1", status: "REVERSED" });
  const d3 = item({ kind: "decision", ref_number: 3, statement: "Marketplace for verified guides" });
  d2.superseded_by_id = d3.id;
  const items = [d2, d3, item({ kind: "question", statement: "Public or invite-only?", status: "OPEN" })];

  it("hides history by default and shows it struck through with a pointer to the replacement", async () => {
    const onSelect = vi.fn();
    const { rerender } = render(<KnowledgeSections items={items} showHistory={false} onSelect={onSelect} />);
    expect(screen.queryByText("No marketplace in v1")).toBeNull();
    expect(screen.getByText("1 in history")).toBeInTheDocument();
    rerender(<KnowledgeSections items={items} showHistory onSelect={onSelect} />);
    const old = screen.getByText("No marketplace in v1");
    expect(old.className).toMatch(/line-through/);
    expect(screen.getAllByText("D3").length).toBeGreaterThan(1); // own row + "→ D3" pointer
    await userEvent.click(screen.getByText("Public or invite-only?"));
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ label: "Q1" }));
  });
});
