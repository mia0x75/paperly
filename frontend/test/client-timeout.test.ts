/**
 * test/client-timeout.test.ts -- client.request timeout behavior.
 *
 * request() races fetch against a timeout via AbortController: a hung request
 * must surface as ApiError(code 60002, timeout text), never hang forever and
 * never leak a raw AbortError. Normal (fast) requests must be unaffected.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { resetDOM } from "./setup";
import { apiGet, fetchBlob, ApiError, BLOB_TIMEOUT_MS, DEFAULT_TIMEOUT_MS, UPLOAD_TIMEOUT_MS } from "../src/api/client";

/** fetch stub that hangs forever unless aborted (respects AbortSignal like real fetch). */
function stubHangingFetch(): void {
  (globalThis as Record<string, unknown>).fetch = (
    _input: unknown,
    init?: RequestInit,
  ) =>
    new Promise((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => {
        reject(new DOMException("This operation was aborted", "AbortError"));
      });
    });
}

/** fetch stub that resolves immediately with a success envelope. */
function stubFastFetch(): void {
  (globalThis as Record<string, unknown>).fetch = async () => ({
    ok: true,
    status: 200,
    json: async () => ({ code: 0, data: { ok: true } }),
    blob: async () => new Blob(),
  });
}

const suite: TestSuite = {
  name: "client（请求超时）",
  cases: [
    ["悬挂请求在超时后抛 ApiError(60002)，而非永远等待", async () => {
      resetDOM();
      stubHangingFetch();
      let thrown: ApiError | null = null;
      try {
        await apiGet("/kids", undefined, 30);
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assert(thrown !== null, "超时应抛 ApiError");
      assertEqual(thrown?.code, 60002, "超时码应为 60002（与 40100/60001 区分）");
      assert((thrown?.message ?? "").length > 0, "应有可读的超时文案，而非 AbortError 原文");
      assert(!(thrown?.message ?? "").includes("abort"), "不得泄漏 AbortError 原文");
    }],

    ["正常请求不受超时影响（timer 在响应后清理）", async () => {
      resetDOM();
      stubFastFetch();
      const data = await apiGet<{ ok: boolean }>("/kids", undefined, 5000);
      assertEqual(data.ok, true, "快请求应正常解包，不受超时逻辑干扰");
    }],

    ["默认超时与上传超时常量符合约定（30s / 5min）", () => {
      assertEqual(DEFAULT_TIMEOUT_MS, 30_000, "默认 JSON 超时应为 30 秒");
      assertEqual(UPLOAD_TIMEOUT_MS, 5 * 60_000, "上传超时应为 5 分钟");
    }],

    ["fetchBlob 悬挂同样超时为 ApiError(60002)", async () => {
      resetDOM();
      stubHangingFetch();
      let thrown: ApiError | null = null;
      try {
        await fetchBlob("/pages/7/file", 30);
      } catch (e) {
        thrown = e instanceof ApiError ? e : null;
      }
      assert(thrown !== null, "图片流超时应抛 ApiError");
      assertEqual(thrown?.code, 60002, "超时码应为 60002");
      assert(!(thrown?.message ?? "").includes("abort"), "不得泄漏 AbortError 原文");
    }],

    ["fetchBlob 正常返回 Blob 不受影响 + 60s 默认", async () => {
      resetDOM();
      stubFastFetch();
      const blob = await fetchBlob("/pages/7/file");
      assert(blob instanceof Blob, "快请求应正常返回 Blob");
      assertEqual(BLOB_TIMEOUT_MS, 60_000, "图片流默认超时应为 60 秒");
    }],
  ],
};

export default suite;