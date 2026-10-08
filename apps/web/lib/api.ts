// Thin, typed fetch wrapper for the IdeaVault API.
// The browser talks to same-origin `/api/*`, which Next.js rewrites to the Go API,
// so the session cookie stays first-party. Unsafe requests carry the CSRF header.

export const API_BASE = "/api";

export class ApiError extends Error {
  status: number;
  kind: string;
  field?: string;
  requestId?: string;
  constructor(status: number, message: string, kind = "error", field?: string, requestId?: string) {
    super(message);
    this.status = status;
    this.kind = kind;
    this.field = field;
    this.requestId = requestId;
  }
}

type Query = Record<string, string | number | boolean | undefined | null | string[]>;

export function qs(query?: Query): string {
  if (!query) return "";
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === "") continue;
    p.set(k, Array.isArray(v) ? v.join(",") : String(v));
  }
  const s = p.toString();
  return s ? `?${s}` : "";
}

export interface RequestOptions {
  query?: Query;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  /** Send FormData as-is (multipart uploads). */
  form?: FormData;
}

async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json", ...opts.headers };
  let body: BodyInit | undefined;
  if (opts.form) {
    body = opts.form;
  } else if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  if (method !== "GET" && method !== "HEAD") headers["X-IdeaVault-CSRF"] = "1";
  const res = await fetch(`${API_BASE}${path}${qs(opts.query)}`, {
    method,
    headers,
    body,
    credentials: "same-origin",
    signal: opts.signal,
    cache: "no-store",
  });
  if (!res.ok) {
    let msg = res.statusText || "Request failed";
    let kind = "error";
    let field: string | undefined;
    let rid: string | undefined;
    try {
      const j = await res.json();
      msg = j.error ?? msg;
      kind = j.kind ?? kind;
      field = j.field;
      rid = j.request_id;
    } catch {
      /* non-JSON error body */
    }
    if (res.status === 401 && typeof window !== "undefined" && !path.startsWith("/v1/auth")) {
      const next = encodeURIComponent(window.location.pathname + window.location.search);
      // Full reload on purpose: nothing cached from the expired session survives.
      // eslint-disable-next-line @next/next/no-location-assign-relative-destination
      window.location.href = `/login?next=${next}`;
    }
    throw new ApiError(res.status, msg, kind, field, rid);
  }
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get("content-type") ?? "";
  if (ct.includes("application/json")) return (await res.json()) as T;
  return (await res.text()) as unknown as T;
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>("GET", path, opts),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>("POST", path, { ...opts, body }),
  put: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>("PUT", path, { ...opts, body }),
  patch: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>("PATCH", path, { ...opts, body }),
  del: <T>(path: string, opts?: RequestOptions) => request<T>("DELETE", path, opts),
  upload: <T>(path: string, form: FormData, opts?: RequestOptions) => request<T>("POST", path, { ...opts, form }),
};

/** Trigger a browser download of a URL served by the API (same-origin, cookie-authenticated). */
export function downloadFromApi(path: string) {
  const a = document.createElement("a");
  a.href = `${API_BASE}${path}`;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/** Download a client-side string as a file. */
export function downloadText(filename: string, text: string, type = "text/markdown;charset=utf-8") {
  const blob = new Blob([text], { type });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}
