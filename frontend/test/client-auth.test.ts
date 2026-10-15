/**
 * test/client-auth.test.ts -- client.request token auto-attachment and 40100 handling.
 *
 * This bypasses mount.ts's fetch stub (which doesn't record headers) with its own
 * stub to pin each case:
 *   1. with a parent token -> auto-attach Authorization: Bearer;
 *   2. without a token -> no Authorization header (don't send an empty Bearer);
 *   3. auto-attach hitting 40100 -> clear that side's token; the kid side also
 *      navigates to /kid/login (kid session);
 *   4. explicit Authorization -> not overwritten or re-handled (authRequest owns it);
 *   5. 40100 at /auth/login, /kid/auth/login = wrong PIN -> don't clear the token;
 *   6. fetch offline/unreachable -> convert to a friendly ApiError instead of
 *      leaking "Failed to fetch" / "NetworkError when attempting to fetch resource.".
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { resetDOM, testWindow } from "./setup";
import { apiGet, fetchBlob, ApiError } from "../src/api/client";
import { PARENT_TOKEN_KEY } from "../src/lib/parentSession";
import { KID_TOKEN_KEY } from "../src/lib/kidSession";

/** Self-written fetch stub: records full headers, replays by status/body. */
interface FetchLog {
  url: string;
  headers: Record<string, string>;
}

let log: FetchLog[] = [];

function stubFetchOnce(status: number, body: unknown): void {
  (globalThis as Record<string, unknown>).fetch = async (
    input: unknown,
    init?: RequestInit,
  ) => {
    const headers: Record<string, string> = {};
    new Headers(init?.headers ?? undefined).forEach((v, k) => {
      headers[k.toLowerCase()] = v;
    });
    log.push({ url: String(input), headers });
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      blob: async () => new Blob([new Uint8Array([0x89, 0x50])]),
    };
  };
}

const suite: TestSuite = {
  name: "client（token 自动附着 + 40100 处置）",
  cases: [
    ["有家长 token：业务请求自动带 Bearer，不再裸奔", async () => {
      resetDOM();
      log = [];
      testWindow.localStorage.setItem(PARENT_TOKEN_KEY, "jwt-abc");
      stubFetchOnce(200, { code: 0, data: [{ id: 1 }] });
      const data = await apiGet<unknown[]>("/kids");
      assertEqual(data.length, 1, "解包拿到 data");
      assertEqual(log.length, 1, "应只发一次请求");
      assertEqual(log[0]?.headers["authorization"] ?? "", "Bearer jwt-abc", "自动附着家长 token");
      assertEqual(log[0]?.url, "/api/kids", "请求走 /api 前缀");
    }],

    ["无 token：不带 Authorization 头（不发 Bearer 空串）", async () => {
      resetDOM();
      log = [];
      stubFetchOnce(200, { code: 0, data: null });
      await apiGet("/kids");
      assertEqual(log[0]?.headers["authorization"], undefined, "无 token 不附着");
    }],

    ["无 token 请求返回 40100：不触发会话失效、不跳转（引导期匿名请求）", async () => {
      resetDOM();
      log = [];
      // Earlier cases may have navigated; reset to a known location to assert "no navigation".
      testWindow.location.assign("/");
      // Simulates the onboarding wizard's GET /api/subjects before any parent token exists:
      // a 40100 here is a plain error, NOT session expiry -- no clear, no redirect to /login.
      stubFetchOnce(200, { code: 40100, message: "缺少登录凭证" });
      let thrown: ApiError | null = null;
      try {
        await apiGet("/subjects");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assert(thrown !== null, "匿名 40100 应抛 ApiError");
      assertEqual(thrown?.code, 40100, "业务码透传");
      assertEqual(log[0]?.headers["authorization"], undefined, "无 token 不附着");
      assertEqual(testWindow.location.pathname, "/", "无 token 的 40100 不得跳转登录页");
    }],

    ["自动附着遇 40100：清家长 token（会话失效）", async () => {
      resetDOM();
      log = [];
      testWindow.localStorage.setItem(PARENT_TOKEN_KEY, "jwt-expired");
      stubFetchOnce(200, { code: 40100, message: "登录已过期" });
      let thrown: ApiError | null = null;
      try {
        await apiGet("/kids");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assert(thrown !== null, "40100 应抛 ApiError");
      assertEqual(thrown?.code, 40100, "业务码透传");
      assertEqual(
        testWindow.localStorage.getItem(PARENT_TOKEN_KEY),
        null,
        "会话失效应清 token",
      );
    }],

    ["显式 Authorization：不覆盖、不重复处置", async () => {
      resetDOM();
      log = [];
      testWindow.localStorage.setItem(PARENT_TOKEN_KEY, "jwt-session");
      stubFetchOnce(200, { code: 40100, message: "登录已过期" });
      let thrown: ApiError | null = null;
      try {
        const { authRequest } = await import("../src/api/client");
        await authRequest("/me", { method: "GET" }, "jwt-target");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assertEqual(log[0]?.headers["authorization"] ?? "", "Bearer jwt-target", "显式 token 优先");
      // authRequest's own catch clears the token and navigates; asserting the key is
      // cleared covers the explicit path.
      assertEqual(testWindow.localStorage.getItem(PARENT_TOKEN_KEY), null, "authRequest 自行清 token");
    }],

    ["凭证入口的 40100 = PIN 错：不清 token", async () => {
      resetDOM();
      log = [];
      testWindow.localStorage.setItem(PARENT_TOKEN_KEY, "jwt-good");
      stubFetchOnce(200, { code: 40100, message: "账号或 PIN 不正确" });
      let thrown: ApiError | null = null;
      try {
        await apiGet("/auth/login");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assertEqual(thrown?.code, 40100, "业务码透传");
      assertEqual(
        testWindow.localStorage.getItem(PARENT_TOKEN_KEY),
        "jwt-good",
        "凭证校验错不得清 token",
      );
    }],

    ["孩子侧自动附着遇 40100：清孩子 token 并跳 /kid/login", async () => {
      resetDOM();
      log = [];
      testWindow.localStorage.setItem(KID_TOKEN_KEY, "jwt-kid-expired");
      stubFetchOnce(200, { code: 40100, message: "登录已过期" });
      let thrown: ApiError | null = null;
      try {
        await apiGet("/kid/dashboard");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assertEqual(thrown?.code, 40100, "业务码透传");
      assertEqual(
        testWindow.localStorage.getItem(KID_TOKEN_KEY),
        null,
        "孩子会话失效应清 token",
      );
      // The kid side has no "logged-in shell" to stay on -- expiry must land on the
      // login page, not a blank screen.
      assertEqual(
        testWindow.location.pathname,
        "/kid/login",
        "孩子侧 40100 应跳转孩子登录页",
      );
    }],

    ["孩子凭证入口 /kid/auth/login 的 40100 = PIN 错：不清 token、不跳转", async () => {
      resetDOM();
      // The last case navigated to /kid/login; reset first to assert "no navigation".
      testWindow.location.assign("/");
      log = [];
      testWindow.localStorage.setItem(KID_TOKEN_KEY, "jwt-kid");
      stubFetchOnce(200, { code: 40100, message: "账号或 PIN 不正确" });
      let thrown: ApiError | null = null;
      try {
        await apiGet("/kid/auth/login");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assertEqual(thrown?.code, 40100, "业务码透传");
      assertEqual(
        testWindow.localStorage.getItem(KID_TOKEN_KEY),
        "jwt-kid",
        "PIN 错不是会话失效，不清 token",
      );
      assertEqual(testWindow.location.pathname, "/", "凭证错不得跳转");
    }],

    ["fetchBlob：页图凭证走请求头（Authorization），不进 URL", async () => {
      resetDOM();
      log = [];
      testWindow.localStorage.setItem(PARENT_TOKEN_KEY, "jwt-blob");
      stubFetchOnce(200, { code: 0, data: null });
      const blob = await fetchBlob("/pages/11/file");
      assert(blob.size > 0, "应拿到字节流");
      assertEqual(log[0]?.headers["authorization"] ?? "", "Bearer jwt-blob", "自动附着 token");
      assertEqual(log[0]?.url, "/api/pages/11/file", "URL 干净（无 query token）");
    }],

    ["fetchBlob 遇 40100：与 request 同处置，清会话抛错", async () => {
      resetDOM();
      log = [];
      testWindow.localStorage.setItem(PARENT_TOKEN_KEY, "jwt-gone");
      // The real gate returns HTTP 401 + envelope for unauthorized (fetchBlob only
      // watches the !response.ok branch).
      stubFetchOnce(401, { code: 40100, message: "登录已过期" });
      let thrown: ApiError | null = null;
      try {
        await fetchBlob("/pages/11/file");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assertEqual(thrown?.code, 40100, "业务码透传");
      assertEqual(testWindow.localStorage.getItem(PARENT_TOKEN_KEY), null, "会话失效应清 token");
    }],

    ["request 断网：fetch reject 转友好文案，不透传浏览器原始报错", async () => {
      resetDOM();
      log = [];
      (globalThis as Record<string, unknown>).fetch = async () => {
        throw new TypeError("Failed to fetch");
      };
      let thrown: ApiError | null = null;
      try {
        await apiGet("/kids");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assert(thrown !== null, "断网应抛 ApiError 而非裸 TypeError");
      assertEqual(thrown?.code, 60001, "网络层专属错误码");
      assertEqual(thrown?.message, "网络连接失败，请检查网络后重试", "统一友好文案");
      assert(
        !(thrown?.message ?? "").includes("Failed to fetch"),
        "不得泄漏浏览器原始报错到用户可见文案",
      );
    }],

    ["fetchBlob 断网：同样转友好文案（Firefox 文案一并屏蔽）", async () => {
      resetDOM();
      log = [];
      (globalThis as Record<string, unknown>).fetch = async () => {
        throw new TypeError("NetworkError when attempting to fetch resource.");
      };
      let thrown: ApiError | null = null;
      try {
        await fetchBlob("/pages/11/file");
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assert(thrown !== null, "断网应抛 ApiError 而非裸 TypeError");
      assertEqual(thrown?.code, 60001, "网络层专属错误码");
      assertEqual("网络连接失败，请检查网络后重试", thrown?.message, "统一友好文案");
    }],
  ],
};

export default suite;
