import { describe, expect, it } from "vitest";
import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { Dropzone } from "@/features/imports/dropzone";

function Harness({ onPicked }: { onPicked?: (f: File | null) => void }) {
  const [file, setFile] = useState<File | null>(null);
  return (
    <Dropzone
      file={file}
      onFile={(f) => {
        setFile(f);
        onPicked?.(f);
      }}
    />
  );
}

const pick = (input: HTMLElement, files: File[]) => fireEvent.change(input, { target: { files } });

describe("Dropzone", () => {
  it("is a keyboard-reachable button before a file is chosen", () => {
    render(<Harness />);
    const zone = screen.getByRole("button", { name: /choose a file to import/i });
    expect(zone).toHaveAttribute("tabindex", "0");
    expect(screen.getByText(/up to 200 MB/)).toBeInTheDocument();
  });

  it("accepts a supported export and describes it", () => {
    render(<Harness />);
    pick(screen.getByTestId("file-input"), [new File(['[{"title":"x"}]'], "conversations.json", { type: "application/json" })]);
    expect(screen.getByText("conversations.json")).toBeInTheDocument();
    expect(screen.getByText(/Looks like a ChatGPT or Claude export/)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("flags unsupported and empty files", () => {
    render(<Harness />);
    pick(screen.getByTestId("file-input"), [new File(["x"], "photo.png", { type: "image/png" })]);
    expect(screen.getByRole("alert")).toHaveTextContent("“.png” files aren't supported");
    fireEvent.click(screen.getByRole("button", { name: "Remove file" }));
    pick(screen.getByTestId("file-input"), [new File([], "empty.md")]);
    expect(screen.getByRole("alert")).toHaveTextContent("This file is empty.");
  });

  it("takes the first file of a multi-file drop and explains why", () => {
    const picked: (File | null)[] = [];
    render(<Harness onPicked={(f) => picked.push(f)} />);
    const zone = screen.getByRole("button", { name: /choose a file to import/i });
    const files = [new File(["a"], "a.md"), new File(["b"], "b.md")];
    fireEvent.drop(zone, { dataTransfer: { files } });
    expect(picked.map((f) => f?.name)).toEqual(["a.md"]);
    expect(screen.getByText(/2 files dropped — using “a.md”/)).toBeInTheDocument();
  });
});
