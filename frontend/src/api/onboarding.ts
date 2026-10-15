import { apiGet, apiPost } from "./client";
import type { MeParent } from "./types";

/** OnboardingStatus is the response of GET /api/onboarding/status. */
export interface OnboardingStatus {
  done: boolean;
}

/**
 * OnboardingComplete is the response of POST /api/onboarding/complete.
 * token/user are optional: completing may issue a default parent JWT in passing
 * (stored to skip manual login); absent when no parent exists (fall back to login).
 */
export interface OnboardingComplete extends OnboardingStatus {
  token?: string;
  user?: MeParent;
}

/**
 * onboardingApi — first-run onboarding (/welcome) status query and completion marker.
 *
 * Status for the route guard's /welcome decision; complete() for the wizard finale.
 */
export const onboardingApi = {
  status(): Promise<OnboardingStatus> {
    return apiGet<OnboardingStatus>("/onboarding/status");
  },
  complete(): Promise<OnboardingComplete> {
    return apiPost<OnboardingComplete>("/onboarding/complete");
  },
};