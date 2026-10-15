import { onboardingApi } from "../api/onboarding";

/**
 * First-run onboarding state for route guards.
 *
 * Only `true` is cached (terminal, never rolls back); `false`/failure always
 * re-queries — a stale cache right after `complete()` would bounce the user back
 * to /welcome. Cache hits skip the request entirely.
 */
let cachedDone = false;

/** Whether onboarding completed; null on request failure. */
export async function isOnboardingDone(): Promise<boolean | null> {
  if (cachedDone) return true;
  try {
    const st = await onboardingApi.status();
    if (st.done) cachedDone = true;
    return st.done;
  } catch {
    return null;
  }
}

/** Invalidate after complete (next navigation re-fetches). */
export function resetOnboardingCache(): void {
  cachedDone = false;
}