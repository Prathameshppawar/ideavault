// Small line-based diff (longest common subsequence) used to compare artifact versions.
// No dependencies: common prefix/suffix are trimmed first, then an LCS table is built over
// the remaining middle. Inputs too large for the table fall back to "all removed, all added".

export type DiffOp = "equal" | "add" | "del";

export interface DiffLine {
  op: DiffOp;
  text: string;
  /** 1-based line number in the old text (equal/del). */
  a?: number;
  /** 1-based line number in the new text (equal/add). */
  b?: number;
}

export type DiffChunk = { kind: "lines"; lines: DiffLine[] } | { kind: "skip"; lines: DiffLine[] };

/** Above this many LCS cells (~16 MB of Uint32) we skip the table and emit a coarse diff. */
const MAX_CELLS = 4_000_000;

export function splitLines(text: string): string[] {
  if (!text) return [];
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  if (lines.length > 1 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

export function diffLines(oldText: string, newText: string): DiffLine[] {
  const A = splitLines(oldText);
  const B = splitLines(newText);
  const out: DiffLine[] = [];

  let start = 0;
  while (start < A.length && start < B.length && A[start] === B[start]) {
    out.push({ op: "equal", text: A[start], a: start + 1, b: start + 1 });
    start++;
  }
  let endA = A.length;
  let endB = B.length;
  while (endA > start && endB > start && A[endA - 1] === B[endB - 1]) {
    endA--;
    endB--;
  }

  const n = endA - start;
  const m = endB - start;
  const middle: DiffLine[] = [];
  if (n > 0 && m > 0 && (n + 1) * (m + 1) <= MAX_CELLS) {
    const w = m + 1;
    const dp = new Uint32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        dp[i * w + j] = A[start + i] === B[start + j] ? dp[(i + 1) * w + j + 1] + 1 : Math.max(dp[(i + 1) * w + j], dp[i * w + j + 1]);
      }
    }
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
      if (A[start + i] === B[start + j]) {
        middle.push({ op: "equal", text: A[start + i], a: start + i + 1, b: start + j + 1 });
        i++;
        j++;
      } else if (dp[(i + 1) * w + j] >= dp[i * w + j + 1]) {
        middle.push({ op: "del", text: A[start + i], a: start + i + 1 });
        i++;
      } else {
        middle.push({ op: "add", text: B[start + j], b: start + j + 1 });
        j++;
      }
    }
    for (; i < n; i++) middle.push({ op: "del", text: A[start + i], a: start + i + 1 });
    for (; j < m; j++) middle.push({ op: "add", text: B[start + j], b: start + j + 1 });
  } else {
    for (let i = 0; i < n; i++) middle.push({ op: "del", text: A[start + i], a: start + i + 1 });
    for (let j = 0; j < m; j++) middle.push({ op: "add", text: B[start + j], b: start + j + 1 });
  }
  out.push(...middle);

  for (let k = 0; endA + k < A.length; k++) {
    out.push({ op: "equal", text: A[endA + k], a: endA + k + 1, b: endB + k + 1 });
  }
  return out;
}

export function diffStats(lines: DiffLine[]): { added: number; removed: number; unchanged: number } {
  let added = 0;
  let removed = 0;
  let unchanged = 0;
  for (const l of lines) {
    if (l.op === "add") added++;
    else if (l.op === "del") removed++;
    else unchanged++;
  }
  return { added, removed, unchanged };
}

/**
 * Groups a diff into visible chunks, folding long unchanged runs into "skip" chunks
 * while keeping `context` lines around every change.
 */
export function collapseUnchanged(lines: DiffLine[], context = 3): DiffChunk[] {
  const keep = new Array<boolean>(lines.length).fill(false);
  lines.forEach((l, i) => {
    if (l.op === "equal") return;
    for (let k = Math.max(0, i - context); k <= Math.min(lines.length - 1, i + context); k++) keep[k] = true;
  });
  const chunks: DiffChunk[] = [];
  let i = 0;
  while (i < lines.length) {
    const visible = keep[i];
    const run: DiffLine[] = [];
    while (i < lines.length && keep[i] === visible) run.push(lines[i++]);
    // A tiny fold isn't worth a click: show runs of 1–2 unchanged lines inline.
    if (!visible && run.length <= 2) appendLines(chunks, run);
    else if (visible) appendLines(chunks, run);
    else chunks.push({ kind: "skip", lines: run });
  }
  return chunks;
}

function appendLines(chunks: DiffChunk[], run: DiffLine[]) {
  const last = chunks[chunks.length - 1];
  if (last && last.kind === "lines") last.lines.push(...run);
  else chunks.push({ kind: "lines", lines: [...run] });
}
