/**
 * kidSession -- persistence of the kid-side session (JWT) and profile (name / kid id).
 *
 * The credential is paperly.kid_token: written after invite/login PIN success; the route
 * guard reads it **synchronously** (an async store would force two-phase guards); the
 * request layer attaches it and clears it on 40100. Same design line as parentSession.
 *
 * Why localStorage, not Pinia: a store rebuilds every refresh and needs re-hydration;
 * storage's first read is final, no async dance. Only the top-bar name is wrapped
 * reactively (stores/kidSession.ts); this file is pure persistence.
 *
 * Boundaries: plaintext JWT is fine (single-machine, no XSS surface; httpOnly needs
 * backend cooperation). Unreadable storage returns "" and the guard routes to /kid/login.
 */

export const KID_TOKEN_KEY = "paperly.kid_token";
export const KID_NAME_KEY = "paperly.kid_name";
export const KID_ID_KEY = "paperly.kid_id";
export const KID_GRADE_KEY = "paperly.kid_grade";
export const KID_TRACK_KEY = "paperly.kid_track";

/** Safe storage read: SSR / test environments may lack localStorage. */
function storage(): Storage | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage;
  } catch {
    // Accessing localStorage throws SecurityError in e.g. Safari private mode.
    return null;
  }
}

/** Kid session token; "" if none (empty suits both the guard's boolean test and "Bearer " building). */
export function readKidToken(): string {
  try {
    return storage()?.getItem(KID_TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

/** Write the kid token (empty string ignored — never store an "empty session"). */
export function writeKidToken(token: string): void {
  if (!token) return;
  try {
    storage()?.setItem(KID_TOKEN_KEY, token);
  } catch {
    // Private mode cannot write: the guard will block again, so staying silent is fine.
  }
}

/** Clear the kid token (401 expiry / explicit logout). */
export function clearKidToken(): void {
  try {
    storage()?.removeItem(KID_TOKEN_KEY);
  } catch {
    // Best effort; leave it to the route guard.
  }
}

/**
 * redirectToKidLogin -- full-page redirect to kid login after session expiry.
 *
 * Full-page, not router.push: 401 handling is centralized and components shouldn't
 * couple to vue-router. Skipped when already on /kid/login (no self-loop).
 */
export function redirectToKidLogin(): void {
  try {
    if (typeof window === "undefined" || window.location.pathname.startsWith("/kid/login")) return;
    if (typeof window.location.assign === "function") {
      window.location.assign("/kid/login");
    } else {
      window.location.href = "/kid/login";
    }
  } catch {
    // No usable window in test/SSR: the token is already cleared, leave it to the guard to route to /kid/login.
  }
}

/** Kid-side session name; "" if none (top bar reads it once, survives refresh). */
export function readKidName(): string {
  try {
    return storage()?.getItem(KID_NAME_KEY) ?? "";
  } catch {
    return "";
  }
}

/** Remember the kid name (empty clears it). */
export function saveKidName(name: string): void {
  try {
    const s = storage();
    if (s === null) return;
    if (name === "") s.removeItem(KID_NAME_KEY);
    else s.setItem(KID_NAME_KEY, name);
  } catch {
    // A failed write must not crash the caller: a missing top-bar name only means it is not shown, not fatal.
  }
}

/** Session kid id; null if none. Lets the list filter by kid_id without fetching parent-side /api/kids. */
export function readKidId(): number | null {
  try {
    const raw = storage()?.getItem(KID_ID_KEY) ?? "";
    if (raw === "") return null;
    const n = Number(raw);
    return Number.isInteger(n) && n > 0 ? n : null;
  } catch {
    return null;
  }
}

/** Remember the kid id (invalid clears it). */
export function saveKidId(id: number | null): void {
  try {
    const s = storage();
    if (s === null) return;
    if (id === null || !Number.isInteger(id) || id <= 0) s.removeItem(KID_ID_KEY);
    else s.setItem(KID_ID_KEY, String(id));
  } catch {
    // Restorable on next /api/me query.
  }
}

/** Session grade; 0 = unset. Defaults entry/list grade without fetching /api/kids. */
export function readKidGrade(): number {
  try {
    const raw = storage()?.getItem(KID_GRADE_KEY) ?? "";
    if (raw === "") return 0;
    const n = Number(raw);
    return Number.isInteger(n) && n > 0 ? n : 0;
  } catch {
    return 0;
  }
}

/** Remember the grade (invalid clears it). */
export function saveKidGrade(grade: number): void {
  try {
    const s = storage();
    if (s === null) return;
    if (!Number.isInteger(grade) || grade <= 0) s.removeItem(KID_GRADE_KEY);
    else s.setItem(KID_GRADE_KEY, String(grade));
  } catch {
    // Restorable on next /api/me query.
  }
}

/** Session track; 0 = undecided. Survives refresh for upload/radar filtering. */
export function readKidTrack(): number {
  try {
    const raw = storage()?.getItem(KID_TRACK_KEY) ?? "";
    if (raw === "") return 0;
    const n = Number(raw);
    return n === 1 || n === 2 ? n : 0;
  } catch {
    return 0;
  }
}

/** Remember the track (invalid clears to undecided). */
export function saveKidTrack(track: number): void {
  try {
    const s = storage();
    if (s === null) return;
    if (track === 1 || track === 2) s.setItem(KID_TRACK_KEY, String(track));
    else s.removeItem(KID_TRACK_KEY);
  } catch {
    // Restorable on next /api/me query.
  }
}
