import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Markdown } from "@/components/markdown";

describe("Markdown", () => {
  it("never renders raw HTML (XSS) and turns iv:// links into entity chips", () => {
    const id = "023d5bfe-045e-4021-b0f1-7ac18d7694d8";
    const { container } = render(
      <Markdown>{`Hello <img src=x onerror="alert(1)"> <script>alert(2)</script>\n\nSee [D3](iv://decision/${id}) and [bad](javascript:alert(1)).`}</Markdown>,
    );
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    const chip = screen.getByRole("link", { name: /D3/ });
    expect(chip).toHaveAttribute("href", `/knowledge/${id}`);
    const bad = screen.getByText("bad");
    expect(bad.closest("a")?.getAttribute("href") ?? "").not.toContain("javascript");
  });
});
