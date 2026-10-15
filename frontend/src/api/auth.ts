import { apiGet, apiPost, authRequest } from "./client";
import { readParentToken } from "../lib/parentSession";
import type {
  AccountSummary,
  InviteSetupResult,
  InviteVerifyResult,
  KidAccount,
  KidLoginResult,
  MeKid,
  MeParent,
  ParentChangePinResult,
  ParentLoginResult,
} from "./types";

/**
 * authApi -- login-state endpoints (parent + kid).
 *
 * - login / accounts / kidAccounts / inviteVerify / inviteSetup go through anonymous
 *   entrypoints (no token before login; a 40100 wrong-PIN is only a credential error,
 *   never "clear session and redirect");
 * - meParent / renameParent / changePin go through authRequest (auto Bearer; 40100
 *   clears the token and redirects to /login, callers handle nothing twice);
 * - kidLogin / meKid go through apiGet/apiPost (request layer attaches the kid token
 *   by route; 40100 clears paperly.kid_token and redirects to /kid/login).
 */
export const authApi = {
  /** GET /api/auth/parent-accounts: loggable parents (active only, with pin_set); anonymous. */
  accounts(): Promise<AccountSummary[]> {
    return apiGet<AccountSummary[]>("/auth/parent-accounts");
  },

  /** POST /api/auth/login: parent PIN login, returns {token, user} on success. */
  login(userID: number, pin: string): Promise<ParentLoginResult> {
    return apiPost<ParentLoginResult>("/auth/login", { user_id: userID, pin });
  },

  /**
   * GET /api/auth/kid-accounts: loggable kid list (anonymous). No PIN status —
   * placeholder-PIN kids list too, login fails uniformly (no profile leak).
   */
  kidAccounts(): Promise<KidAccount[]> {
    return apiGet<KidAccount[]>("/auth/kid-accounts");
  },

  /**
   * POST /api/kid/auth/login: kid PIN login, returns {token, kid}. Body field is
   * user_id (shared body; the kid case passes kid_id). 40100 wrong PIN, 42900
   * rate-limited; pages map to messages.
   */
  kidLogin(kidId: number, pin: string): Promise<KidLoginResult> {
    return apiPost<KidLoginResult>("/kid/auth/login", { user_id: kidId, pin });
  },

  /** POST /api/kid/invite/verify: verify the invite code; an invalid code returns {valid:false} (still 200). */
  inviteVerify(code: string): Promise<InviteVerifyResult> {
    return apiPost<InviteVerifyResult>("/kid/invite/verify", { code });
  },

  /** POST /api/kid/invite/setup: set the PIN and directly issue the kid JWT. */
  inviteSetup(code: string, pin: string): Promise<InviteSetupResult> {
    return apiPost<InviteSetupResult>("/kid/invite/setup", { code, pin });
  },

  /** GET /api/me: current logged-in kid (kid audience returns MeKid). Token is attached automatically by the request layer per route. */
  meKid(): Promise<MeKid> {
    return apiGet<MeKid>("/me");
  },

  /** GET /api/me: current logged-in parent (parent audience returns MeParent). */
  meParent(): Promise<MeParent> {
    return authRequest<MeParent>("/me", { method: "GET" }, readParentToken());
  },

  /** POST /api/me/rename: change the current parent's nickname, returns the updated profile (parent audience only). */
  renameParent(displayName: string): Promise<MeParent> {
    return authRequest<MeParent>(
      "/me/rename",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ display_name: displayName }),
      },
      readParentToken(),
    );
  },

  /** POST /api/me/pin: change PIN (pass an empty oldPin on first setup), returns the new JWT. */
  changePin(oldPin: string, newPin: string): Promise<ParentChangePinResult> {
    return authRequest<ParentChangePinResult>(
      "/me/pin",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ old_pin: oldPin, new_pin: newPin }),
      },
      readParentToken(),
    );
  },
};