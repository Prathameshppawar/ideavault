import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { DiffView } from "@/features/artifacts/artifact-history";
import { diffLines } from "@/features/artifacts/line-diff";

const before = Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join("\n");
const after = before.replace("line 15", "line fifteen (revised)").concat("\nline 31");

describe("DiffView", () => {
  it("marks added and removed lines and folds unchanged runs", () => {
    render(<DiffView lines={diffLines(before, after)} collapse />);
    expect(screen.getAllByLabelText("added")).toHaveLength(2);
    expect(screen.getAllByLabelText("removed")).toHaveLength(1);
    expect(screen.getByText("line fifteen (revised)")).toBeInTheDocument();
    const fold = screen.getByRole("button", { name: /11 unchanged lines/ });
    expect(screen.queryByText("line 1")).toBeNull();
    fireEvent.click(fold);
    expect(screen.getByText("line 1")).toBeInTheDocument();
  });

  it("shows every line when not collapsed", () => {
    render(<DiffView lines={diffLines("a\nb", "a\nc")} collapse={false} />);
    expect(screen.getAllByRole("row")).toHaveLength(3);
  });
});
