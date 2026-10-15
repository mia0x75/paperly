import { clearParentToken, readParentToken, redirectToLogin } from "../lib/parentSession";
import { clearKidToken, readKidToken, redirectToKidLogin } from "../lib/kidSession";

/** Backend unified success response envelope: {"code":0,"data":...}. */
interface ApiEnvelope<T> {
  code: number;
  data: T;
}

/** Backend unified error response body: {"code":...,"message":"...","detail":"..."}. */
interface ApiErrorBody {
  code: number;
  message: string;
  detail?: string;
}

/** ApiError is a frontend-recognizable business error (carrying the backend biz code and detail). */
export class ApiError extends Error {
  readonly code: number;
  readonly detail: string;

  constructor(code: number, message: string, detail = "") {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.detail = detail;
  }
}

/** Prefix for all requests (relative path, same origin). */
const BASE_URL = "/api";

/** Auth biz code for session expiry (middleware uses 40100 uniformly; token invalid/expired/pv mismatch). */
const CODE_UNAUTHORIZED = 40100;

/** Frontend-only code for network-layer failure (offline/service unreachable/CORS; no corresponding backend envelope). */
const CODE_NETWORK = 60001;

/**
 * Frontend-only code for request timeout (no corresponding backend envelope).
 * The string name of this code is 'timeout'; kept numeric to satisfy ApiError.code.
 */
const CODE_TIMEOUT = 60002;

/** Default timeout for JSON APIs: a hung request resolves as a timeout error instead of hanging forever. */
export const DEFAULT_TIMEOUT_MS = 30_000;

/** Upload timeout: multipart uploads carry whole exam volumes and legitimately take longer. */
export const UPLOAD_TIMEOUT_MS = 5 * 60_000;

/** Blob timeout: page images are large single files; wider than the JSON default. */
export const BLOB_TIMEOUT_MS = 60_000;

/** Unified display text for network-layer failure: masks the browser's raw error ("Failed to fetch" / "NetworkError..."). */
const NETWORK_ERROR_MESSAGE = "网络连接失败，请检查网络后重试";

/** Unified display text for request timeout: the AbortError text ("The operation was aborted") never reaches the screen. */
const TIMEOUT_ERROR_MESSAGE = "请求超时，请稍后重试";

/**
 * networkError builds an ApiError for network-layer failure.
 *
 * fetch's rejection text varies by browser and language ("Failed to fetch" /
 * "NetworkError..." / "Load failed") — convert uniformly to readable text, never
 * write the raw error to detail (it would leak into panels like task details).
 */
function networkError(): ApiError {
  return new ApiError(CODE_NETWORK, NETWORK_ERROR_MESSAGE);
}

function timeoutError(): ApiError {
  return new ApiError(CODE_TIMEOUT, TIMEOUT_ERROR_MESSAGE);
}

/** Token side: which key to clear and which login page on 40100. */
type TokenSide = "parent" | "kid";

/**
 * isKidPath precisely identifies a "kid-side path": `/kid` itself or a `/kid/*` subpath.
 *
 * The request layer can't read router context, so it matches the path here — the same
 * rule as the route's `meta.audience === "kid"`, not a second standard.
 */
function isKidPath(path: string): boolean {
  return path === "/kid" || path.startsWith("/kid/");
}

/**
 * chooseKid -- select the token to attach for this request.
 *
 * kidTokenJWT and parentTokenJWT are independent; whichever is present is carried
 * (kid route -> kid token, else parent). Both present: the URL prefix decides.
 * Neither: empty (backend gate 40100s it).
 *
 * Prefix must match the `/kid` segment exactly: `/kids*` also starts with "/kid",
 * and startsWith would misjudge parent-side kid pages as kid routes (same pitfall
 * as router/index.ts's isKidPath).
 */
function chooseKid(): { token: string; side: TokenSide } {
  const parent = readParentToken();
  const kid = readKidToken();
  if (parent !== "" && kid !== "") {
    const onKidRoute = typeof window !== "undefined" && isKidPath(window.location.pathname);
    return onKidRoute ? { token: kid, side: "kid" } : { token: parent, side: "parent" };
  }
  if (parent !== "") return { token: parent, side: "parent" };
  return { token: kid, side: "kid" };
}

/** isCredentialPath is a "credential verification" entrypoint: 40100 = wrong PIN, not session expiry, so do not clear the token. */
function isCredentialPath(path: string): boolean {
  const clean = path.split("?")[0] ?? path;
  return clean === "/auth/login" || clean === "/kid/auth/login";
}

/**
 * handleSessionExpired clears the token on the corresponding side, then full-page
 * redirects (parent -> /login with return param; kid -> /kid/login).
 */
function handleSessionExpired(side: TokenSide): void {
  if (side === "kid") {
    clearKidToken();
    redirectToKidLogin();
    return;
  }
  clearParentToken();
  redirectToLogin();
}

/**
 * withQuery builds a query object into "?k=v&...".
 * undefined / null / empty skipped; arrays expand as repeated keys; objects JSON-serialize.
 * URLSearchParams guarantees escaping (replaces ofetch's built-in).
 */
function withQuery(path: string, query?: Record<string, unknown>): string {
  if (!query) return path;
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") continue;
    if (Array.isArray(value)) {
      for (const item of value) params.append(key, String(item));
      continue;
    }
    if (typeof value === "object") {
      params.append(key, JSON.stringify(value));
      continue;
    }
    params.append(key, String(value));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

/** isApiErrorBody checks for a {code,message} shape (business error vs gateway error page). */
function isApiErrorBody(body: unknown): body is ApiErrorBody {
  return body !== null && typeof body === "object" && "code" in body;
}

/**
 * request is the unified request entrypoint: send request -> parse JSON -> unwrap the {code,data} envelope -> convert to ApiError.
 *
 * Semantics aligned with the old ofetch wrapper:
 *   - code=0 returns data, else ApiError (falls back to HTTP status when code is missing)
 *   - non-JSON responses (e.g. HTML error pages) also become ApiError, never a parse exception
 *   - no automatic retry (ofetch retry: 0)
 *
 * Automatic token attachment: no explicit Authorization → chooseKid() supplies one.
 *
 * Unified 40100 handling: the session clears **only when this request's token was
 * auto-attached** —
 *   - authRequest (explicit token) catches its own, no double clear;
 *   - 40100 from a credential entrypoint (/auth/login, /kid/auth/login) = wrong PIN,
 *     not expiry, so not cleared.
 *
 * Decision finalization: the token side is decided once by chooseKid; a 200 finalizes
 * it — clearing only happens in the failed-response 40100 branch, never on success.
 *
 * Timeout: fetch races timeoutMs via AbortController; timeout surfaces as
 * ApiError(CODE_TIMEOUT), never raw AbortError. The timer covers the round trip only
 * (cleared on headers+status, before JSON parsing) and on every path.
 */
async function request<T>(path: string, init: RequestInit, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<T> {
  const headers = new Headers(init.headers ?? undefined);
  const hasExplicitAuth = headers.get("Authorization") !== null;
  let side: TokenSide = "parent";
  // attached: did *this* request carry an auto-attached token? Only then is 40100 real
  // expiry — an anonymous gated call (e.g. onboarding loading subjects) must not clear
  // the session and hard-redirect (would reset the wizard).
  let attached = false;
  if (!hasExplicitAuth) {
    const picked = chooseKid();
    side = picked.side;
    if (picked.token !== "") {
      headers.set("Authorization", `Bearer ${picked.token}`);
      attached = true;
    }
  }

  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), timeoutMs);
  let response: Response;
  try {
    response = await fetch(`${BASE_URL}${path}`, { ...init, headers, signal: ctrl.signal });
  } catch {
    clearTimeout(timer);
    // AbortController fired first: the request timed out (not a network failure).
    if (ctrl.signal.aborted) throw timeoutError();
    // Offline/service unreachable/CORS: fetch rejects directly; the raw browser error never reaches the screen.
    throw networkError();
  }
  clearTimeout(timer);

  // 204: return undefined, don't parse.
  if (response.status === 204) return undefined as T;

  let body: unknown;
  try {
    body = await response.json();
  } catch {
    body = undefined;
  }

  if (response.ok && isApiErrorBody(body) && body.code === 0) {
    return (body as unknown as ApiEnvelope<T>).data;
  }

  const err = isApiErrorBody(body) ? body : undefined;
  if (attached && err?.code === CODE_UNAUTHORIZED && !isCredentialPath(path)) {
    handleSessionExpired(side);
  }
  throw new ApiError(
    typeof err?.code === "number" ? err.code : response.status,
    err?.message ?? `请求失败（HTTP ${response.status}）`,
    err?.detail ?? "",
  );
}

/** jsonInit builds request params for a JSON body (explicitly sets Content-Type). */
function jsonInit(method: "POST" | "PUT", body?: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  };
}

/** apiGet issues a GET and returns the unwrapped data. */
export function apiGet<T>(path: string, query?: Record<string, unknown>, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<T> {
  return request<T>(withQuery(path, query), { method: "GET" }, timeoutMs);
}

/** apiPost issues a POST (JSON body optional) and returns the unwrapped data. */
export function apiPost<T>(path: string, body?: unknown, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<T> {
  return request<T>(path, jsonInit("POST", body), timeoutMs);
}

/** apiPut issues a PUT and returns the unwrapped data. */
export function apiPut<T>(path: string, body?: unknown, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<T> {
  return request<T>(path, jsonInit("PUT", body), timeoutMs);
}

/** apiDelete issues a DELETE and returns the unwrapped data. */
export function apiDelete<T>(path: string, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<T> {
  return request<T>(path, { method: "DELETE" }, timeoutMs);
}

/**
 * apiUploadMany uploads **ordered multiple files** (multipart/form-data).
 *
 * Each file is appended under the same "file" field, and **the order is the
 * array order**—the backend uses the order of form.File["file"] as the page
 * order (page_index), so the sequence in FrontForm is the basis for page
 * numbers, and a reorder must be reflected here.
 */
export function apiUploadMany<T>(
  path: string,
  files: readonly File[],
  fields?: Record<string, string | number | undefined>,
): Promise<T> {
  const form = new FormData();
  for (const f of files) {
    form.append("file", f);
  }
  for (const [k, v] of Object.entries(fields ?? {})) {
    if (v === undefined) continue;
    form.append(k, String(v));
  }
  return request<T>(path, { method: "POST", body: form }, UPLOAD_TIMEOUT_MS);
}

/**
 * fetchBlob -- binary GET with automatic token attachment (byte-stream endpoints such as page images).
 *
 * Subresource requests like `<img :src>` cannot carry an Authorization header—
 * pointing directly at a gated endpoint would surely 401 and break every page
 * image. So page images first fetch the byte stream via this function, then
 * convert to a blob URL fed to <img>; credentials still travel only in headers,
 * never in the URL or the access log.
 *
 * Failure semantics align with request(): 40100 (session expiry, token
 * auto-attached) clears the corresponding side's session and redirects to login;
 * otherwise it throws ApiError (including the backend message). Byte endpoints
 * have no 204/envelope.
 *
 * Timeout: same AbortController pattern as request() (default 60s, wider than
 * JSON — page images are large). The timer covers the network round trip only
 * and is cleared on every path; callers treat timeout like any other fetch
 * failure (ExamViewerPage retries boundedly, then keeps the placeholder).
 */
export async function fetchBlob(path: string, timeoutMs: number = BLOB_TIMEOUT_MS): Promise<Blob> {
  const headers = new Headers();
  const picked = chooseKid();
  if (picked.token !== "") headers.set("Authorization", `Bearer ${picked.token}`);

  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), timeoutMs);
  let response: Response;
  try {
    response = await fetch(`${BASE_URL}${path}`, { method: "GET", headers, signal: ctrl.signal });
  } catch {
    clearTimeout(timer);
    // AbortController fired first: the image fetch timed out (not a network failure).
    if (ctrl.signal.aborted) throw timeoutError();
    // Page-image fetch shares the network-layer fallback text (same semantics as request).
    throw networkError();
  }
  clearTimeout(timer);
  if (!response.ok) {
    let body: unknown;
    try {
      body = await response.json();
    } catch {
      body = undefined;
    }
    const err = isApiErrorBody(body) ? body : undefined;
    if (picked.token !== "" && err?.code === CODE_UNAUTHORIZED) {
      handleSessionExpired(picked.side);
    }
    throw new ApiError(
      typeof err?.code === "number" ? err.code : response.status,
      err?.message ?? `请求失败（HTTP ${response.status}）`,
      err?.detail ?? "",
    );
  }
  return response.blob();
}

/**
 * authRequest -- an authed request with an explicitly specified token (for cases like PIN change that must use the caller's token).
 *
 * Token attachment has moved down into request() (auto-supplies chooseKid() when
 * there is no Authorization); this function's value is **explicit specification**
 * (e.g. PIN change uses the target token from the request body rather than the current session).
 *
 * On session expiry (biz code 40100, token invalid/expired/pv mismatch) it
 * uniformly **clears the token + full-page redirects to /login** preserving the
 * redirect return param, so callers need not each handle expired sessions. Note:
 * business validation 4xx (e.g. "current PIN incorrect" returning 40010 on PIN
 * change) is not in this category and will not be mistakenly cleared.
 */
export function authRequest<T>(path: string, init: RequestInit, token: string): Promise<T> {
  const headers = new Headers(init.headers ?? undefined);
  headers.set("Authorization", `Bearer ${token}`);
  return request<T>(path, { ...init, headers }).catch((e: unknown) => {
    if (e instanceof ApiError && e.code === CODE_UNAUTHORIZED) {
      clearParentToken();
      redirectToLogin();
    }
    throw e;
  });
}
