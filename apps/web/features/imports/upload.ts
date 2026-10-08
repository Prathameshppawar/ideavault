// Multipart upload with progress. `fetch` can't report upload progress, so this uses XHR,
// mirroring lib/api's conventions (same-origin /api, CSRF header, ApiError).
import { API_BASE, ApiError } from "@/lib/api";

export function uploadWithProgress<T>(path: string, form: FormData, onProgress: (fraction: number) => void, signal?: AbortSignal): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${API_BASE}${path}`);
    xhr.withCredentials = true;
    xhr.setRequestHeader("Accept", "application/json");
    xhr.setRequestHeader("X-IdeaVault-CSRF", "1");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) onProgress(e.loaded / e.total);
    };
    xhr.onload = () => {
      let body: unknown;
      try {
        body = xhr.responseText ? JSON.parse(xhr.responseText) : undefined;
      } catch {
        body = undefined;
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        onProgress(1);
        resolve(body as T);
        return;
      }
      const j = (body ?? {}) as { error?: string; kind?: string; field?: string; request_id?: string };
      reject(new ApiError(xhr.status, j.error ?? (xhr.statusText || "Upload failed"), j.kind, j.field, j.request_id));
    };
    xhr.onerror = () => reject(new ApiError(0, "Network error — the upload didn't reach the server."));
    xhr.onabort = () => reject(new ApiError(0, "Upload cancelled.", "aborted"));
    signal?.addEventListener("abort", () => xhr.abort(), { once: true });
    xhr.send(form);
  });
}
