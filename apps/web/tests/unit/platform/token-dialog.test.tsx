import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { CreateTokenDialog } from "@/features/settings/token-dialog";

const post = vi.fn();
vi.mock("@/lib/api", async (orig) => {
  const mod = await orig<typeof import("@/lib/api")>();
  return { ...mod, api: { ...mod.api, post: (...args: unknown[]) => post(...args) } };
});

const SECRET = "ivt_test_0123456789abcdef";

function Harness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button onClick={() => setOpen(true)}>open</button>
      <CreateTokenDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function renderHarness() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
}

describe("API token dialog", () => {
  beforeEach(() => {
    post.mockReset();
    post.mockResolvedValue({ token: SECRET, note: "Store this token now", session: { id: "s1", label: "Raycast", kind: "api_token", expires_at: "2027-10-09T00:00:00Z" } });
  });

  it("shows the token exactly once with a copy button and forgets it on close", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    renderHarness();

    await user.click(screen.getByText("open"));
    await user.type(screen.getByLabelText("Label"), "Raycast");
    await user.click(screen.getByRole("button", { name: /Create token/ }));

    expect(post).toHaveBeenCalledWith("/v1/auth/tokens", { label: "Raycast" });
    const field = await screen.findByLabelText("New API token");
    expect(field).toHaveValue(SECRET);
    expect(screen.getByRole("alert")).toHaveTextContent(/only time the token is shown/i);

    await user.click(screen.getByRole("button", { name: "Copy token" }));
    expect(writeText).toHaveBeenCalledWith(SECRET);
    expect(await screen.findByText("Copied")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /I've stored it/ }));
    await waitFor(() => expect(screen.queryByLabelText("New API token")).not.toBeInTheDocument());

    // Re-opening starts a fresh form; the secret is gone for good.
    await user.click(screen.getByText("open"));
    expect(await screen.findByLabelText("Label")).toHaveValue("");
    expect(screen.queryByDisplayValue(SECRET)).not.toBeInTheDocument();
  });

  it("shows server errors inline", async () => {
    post.mockRejectedValueOnce(new Error("rate limited"));
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("open"));
    await user.click(screen.getByRole("button", { name: /Create token/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("rate limited");
  });
});
