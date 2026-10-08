// Snippet helpers. The API marks matched terms with «guillemets»; we turn them into
// segments rendered as <mark> elements — never HTML strings.

export interface Segment {
  text: string;
  hit: boolean;
}

/**
 * Split a snippet into plain and highlighted segments. Only balanced «…» pairs highlight;
 * stray markers are dropped. Adjacent segments of the same kind are merged.
 */
export function parseHighlights(snippet: string): Segment[] {
  const out: Segment[] = [];
  const push = (text: string, hit: boolean) => {
    if (!text) return;
    const last = out[out.length - 1];
    if (last && last.hit === hit) last.text += text;
    else out.push({ text, hit });
  };
  const re = /«([^«»]*)»/g;
  let i = 0;
  for (let m = re.exec(snippet); m; m = re.exec(snippet)) {
    push(snippet.slice(i, m.index).replace(/[«»]/g, ""), false);
    push(m[1], true);
    i = m.index + m[0].length;
  }
  push(snippet.slice(i).replace(/[«»]/g, ""), false);
  return out;
}

/**
 * Make a raw snippet readable as a single line of prose: drop code fences, box-drawing art,
 * markdown emphasis/heading/link syntax, and collapse whitespace. Keeps «» markers intact.
 */
export function cleanSnippet(raw: string): string {
  return (
    raw
      .replace(/```[a-z]*\n?/gi, " ")
      .replace(/[─-╿]+/g, " ")
      .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
      .replace(/(^|\n)\s{0,3}#{1,6}\s+/g, "$1")
      .replace(/(^|\n)\s*[-*+]\s+/g, "$1")
      .replace(/\*\*|__|`/g, "")
      .replace(/(^|[\s(«])[*_](?=\S)/g, "$1")
      .replace(/(\S)[*_](?=[\s).,;:!?»]|$)/g, "$1")
      .replace(/\s+/g, " ")
      .trim()
  );
}
