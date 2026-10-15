/**
 * test/invite.page.test.ts -- invite setup page (/invite/:code).
 *
 * Covers: a valid code renders "welcome + set PIN", an invalid code gives a
 * unified message and renders no form; verify network/5xx goes to ErrorState +
 * retry; PIN format and confirm-match validation; a successful write of the kid
 * token and redirect to /kid; setup-failure message on screen and retryable.
 *
 * This page does **not** read/write accessCode -- the invite code lives only in
 * the per-request verify/setup body; the session credential is the returned
 * kid JWT (paperly.kid_token).
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { clickButton, findButton, setNativeInput } from "./controls";
import { testWindow } from "./setup";
import { KID_TOKEN_KEY } from "../src/lib/kidSession";
import InvitePage from "../src/pages/InvitePage.vue";

const CODE = "abc123";

async function mountInvite(responses: Record<string, unknown>): Promise<MountResult> {
  return mountPage(InvitePage, { path: `/invite/${CODE}`, responses });
}

/** Fill both PIN fields (controlled inputs; each needs a dispatch input event). */
function fillPins(r: MountResult, pin: string, confirm: string): void {
  setNativeInput(r.doc.querySelector("#invite-pin"), pin);
  setNativeInput(r.doc.querySelector("#invite-confirm"), confirm);
}

const VALID = {
  "POST /api/kid/invite/verify": { valid: true, name: "小明" },
  "POST /api/kid/invite/setup": { token: "jwt-kid", kid: { id: 7, name: "小明", grade: 9 } },
};

const suite: TestSuite = {
  name: "InvitePage（邀请设置）",
  cases: [
    ["有效码：渲染欢迎语与设 PIN 表单，verify 以路由码发起", async () => {
      const r = await mountInvite(VALID);
      await flush();
      assertIncludes(r.text, "欢迎，小明。请设置你的 PIN", "带名字的欢迎语");
      assertIncludes(r.text, "4-6 位数字", "PIN 格式即时提示");
      assert(
        r.calls.some((c) => c.url.includes("/api/kid/invite/verify")),
        `应请求 verify：${r.requests.join(", ")}`,
      );
      const verifyCall = r.calls.find((c) => c.url.includes("/api/kid/invite/verify"));
      assertEqual(verifyCall?.body ?? "", JSON.stringify({ code: CODE }), "verify 载荷为路由码");
      assert(r.doc.querySelector("#invite-pin") !== null, "PIN 输入框在");
      assert(r.errors.length === 0, `控制台错误：${r.errors.join(" | ")}`);
    }],

    ["无效码：统一「邀请码无效」，不渲染表单、不发 setup", async () => {
      const r = await mountInvite({
        "POST /api/kid/invite/verify": { valid: false },
        "POST /api/kid/invite/setup": { token: "x" },
      });
      await flush();
      assertIncludes(r.text, "邀请码无效", "统一文案（不区分码不存在/不可用）");
      assertNotIncludes(r.text, "请设置你的 PIN", "无效码不该出现表单");
      assertEqual(r.doc.querySelector("#invite-pin"), null, "无 PIN 输入框");
      assert(
        !r.requests.some((u) => u.includes("/api/kid/invite/setup")),
        "无效码不得发起 setup",
      );
    }],

    ["verify 失败：ErrorState 上屏，点重试重新校验", async () => {
      const r = await mountInvite({
        "POST /api/kid/invite/verify": { code: 50001, message: "服务不可用" },
      });
      await flush();
      assertIncludes(r.text, "服务不可用", "错误文案透传");
      clickButton(r.doc, "重试");
      await flush();
      const verifies = r.requests.filter((u) => u.includes("/api/kid/invite/verify"));
      assertEqual(verifies.length, 2, "重试应再发一次 verify");
      assertIncludes(r.text, "服务不可用", "重试仍失败时错误继续上屏");
    }],

    ["PIN 格式非法：提交上屏提示，不发 setup", async () => {
      const r = await mountInvite(VALID);
      await flush();
      fillPins(r, "123", "123");
      await flush();
      clickButton(r.doc, "进入学习空间");
      await flush();
      assertIncludes(r.text, "PIN 须为 4-6 位数字", "格式错误提示");
      assert(
        !r.requests.some((u) => u.includes("/api/kid/invite/setup")),
        "校验不过不得发起 setup",
      );
    }],

    ["二次确认不一致：确认框下即时提示，不等提交", async () => {
      const r = await mountInvite(VALID);
      await flush();
      fillPins(r, "123456", "654321");
      await flush();
      assertIncludes(r.text, "两次输入的 PIN 不一致", "即时 mismatch 提示");
    }],

    ["提交成功：写孩子 token 并跳 /kid，载荷为 {code, pin}", async () => {
      const r = await mountInvite(VALID);
      await flush();
      fillPins(r, "123456", "123456");
      await flush();
      clickButton(r.doc, "进入学习空间");
      await flush();
      const setup = r.calls.find((c) => c.url.includes("/api/kid/invite/setup"));
      assert(setup !== undefined, "应发起 setup");
      assertEqual(setup?.method ?? "", "POST", "setup 走 POST");
      assertEqual(
        setup?.body ?? "",
        JSON.stringify({ code: CODE, pin: "123456" }),
        "setup 载荷",
      );
      assertEqual(
        testToken(),
        "jwt-kid",
        "成功后写入孩子会话 token",
      );
      assert(r.navigations.includes("/kid"), `应跳 /kid：${r.navigations.join(" → ")}`);
    }],

    ["setup 失败（限流文案透传）：红字上屏、token 不写、按钮恢复可点", async () => {
      const r = await mountInvite({
        "POST /api/kid/invite/verify": { valid: true, name: "小明" },
        "POST /api/kid/invite/setup": { code: 42900, message: "尝试次数过多，请稍后" },
      });
      await flush();
      fillPins(r, "123456", "123456");
      await flush();
      clickButton(r.doc, "进入学习空间");
      await flush();
      assertIncludes(r.text, "尝试次数过多，请稍后", "后端 message 上屏");
      assertEqual(testToken(), null, "失败不得写 token");
      assert(!r.navigations.includes("/kid"), "失败不得跳转");
      const btn = findButton(r.doc, "进入学习空间") as unknown as { disabled: boolean };
      assertEqual(btn.disabled, false, "失败后应可重新提交");
    }],
  ],
};

/** Read the current kid token (for assertions; the key matches lib/kidSession). */
function testToken(): string | null {
  return testWindow.localStorage.getItem(KID_TOKEN_KEY);
}

export default suite;
