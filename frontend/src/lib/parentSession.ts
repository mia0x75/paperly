/**
 * lib/parentSession -- persistence of the parent login-state JWT.
 *
 * Why localStorage, not Pinia: the guard checks token presence synchronously
 * (async hydration would force two-phase guards). Why token only, not "who am I":
 * a stored profile goes stale across logout/switch and would render old nicknames;
 * nickname consumers go through GET /api/me uniformly.
 *
 * Boundaries: plaintext JWT is fine (single-machine, no XSS surface; httpOnly needs
 * backend cooperation). Unreadable returns "" — callers block routes instead of
 * issuing doomed 401s.
 */

export const PARENT_TOKEN_KEY = "paperly.parent_token";

/** Safe storage read: SSR / test environments may lack localStorage. */
function storage(): Storage | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage;
  } catch {
    // Accessing localStorage throws SecurityError in e.g. Safari private mode.
    return null;
  }
}

/** Parent session token; "" if none. */
export function readParentToken(): string {
  try {
    return storage()?.getItem(PARENT_TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

/** Write the parent token (login / PIN rotation / onboarding). */
export function writeParentToken(token: string): void {
  if (!token) return;
  try {
    storage()?.setItem(PARENT_TOKEN_KEY, token);
  } catch {
    // Private mode cannot write: the guard will block again when login state fails, so staying silent is fine.
  }
}

/** Clear the parent token (401 expiry / explicit logout). */
export function clearParentToken(): void {
  try {
    storage()?.removeItem(PARENT_TOKEN_KEY);
  } catch {
    // Same as above, best effort.
  }
}

/**
 * loginRedirectUrl -- post-expiry return-to-login URL (pure, unit-testable).
 * Null when already on /login (no self-loop); else /login?redirect=<fullPath>.
 */
export function loginRedirectUrl(fullPath: string): string | null {
  if (fullPath.startsWith("/login")) return null;
  return `/login?redirect=${encodeURIComponent(fullPath)}`;
}

/**
 * redirectToLogin -- full-page redirect after session expiry, with redirect param.
 * Full-page, not router.push: expiry can happen in any component; location needs
 * no router instance.
 */
export function redirectToLogin(): void {
  try {
    const target = loginRedirectUrl(`${window.location.pathname}${window.location.search}`);
    if (target === null) return;
    if (typeof window.location.assign === "function") {
      window.location.assign(target);
    } else {
      window.location.href = target;
    }
  } catch {
    // No usable window in test/SSR: the token is already cleared, leave it to the guard to route to /login.
  }
}