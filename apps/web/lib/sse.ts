// Minimal Server-Sent Events client over fetch POST (EventSource only supports GET).
import { API_BASE, ApiError } from "./api";

export interface SSEMessage {
  event: string;
  data: unknown;
}

/** Parse an SSE byte stream into events. Exported for unit tests. */
export function createSSEParser(onMessage: (m: SSEMessage) => void) {
  let buffer = "";
  return (chunk: string) => {
    buffer += chunk.replace(/\r\n/g, "\n");
    let idx: number;
    while ((idx = buffer.indexOf("\n\n")) >= 0) {
      const block = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      let event = "message";
      const dataLines: string[] = [];
      for (const line of block.split("\n")) {
        if (line.startsWith(":")) continue; // comment / keep-alive
        if (line.startsWith("event:")) event = line.slice(6).trim();
        else if (line.startsWith("data:")) dataLines.push(line.slice(5).replace(/^ /, ""));
      }
      if (dataLines.length === 0) continue;
      const raw = dataLines.join("\n");
      let data: unknown = raw;
      try {
        data = JSON.parse(raw);
      } catch {
        /* keep raw text */
      }
      onMessage({ event, data });
    }
  };
}

/** POST a JSON body and stream SSE events until the stream ends or the signal aborts. */
export async function postSSE(path: string, body: unknown, onMessage: (m: SSEMessage) => void, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "text/event-stream", "X-IdeaVault-CSRF": "1" },
    body: JSON.stringify(body),
    credentials: "same-origin",
    signal,
  });
  if (!res.ok || !res.body) {
    let msg = res.statusText || "Request failed";
    try {
      const j = await res.json();
      msg = j.error ?? msg;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, msg);
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  const parse = createSSEParser(onMessage);
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      parse(decoder.decode(value, { stream: true }));
    }
  } finally {
    reader.releaseLock();
  }
}
