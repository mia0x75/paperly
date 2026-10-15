/**
 * test/login.page.test.ts -- parent login page (/login).
 *
 * Covers: the account list rendered grouped by "PIN set / PIN not set", the
 * not-set group entirely unselectable with a hint, PIN input stripping non-digits,
 * successful login writing the token and redirecting back per redirect, wrong
 * credentials shown, and the login button disabled when no account is selected / the PIN is short.
 */
import { assert, assertEqual, assertIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { findButton, clickButton, setNativeInput } from "./controls";
import { testWindow } from "./setup";
import LoginPage from "../src/pages/LoginPage.vue";

// GET /api/auth/parent-accounts uses the AccountSummary view: no status (the server-side
// ListAccounts already filters out non-active), so disabled accounts never appear in the response.
const MOM = { id: 2, display_name: "妈妈", pin_set: true };
const DAD = { id: 3, display_name: "爸爸", pin_set: false };

async function mountLogin(
  responses: Record<string, unknown>,
  path = "/login?redirect=%2Fstorage",
  storage: Record<string, string> = {},
): Promise<MountResult> {
  return mountPage(LoginPage, { path, responses, storage });
}

/**
 * Select an option in the account dropdown (native select, driven by the change
 * event via v-model).
 */
function pickAccount(doc: unknown, label: string): void {
  // r.doc is happy-dom's Document, structurally incompatible with lib.dom's;
  // follow mount.ts's convention, closing it off with unknown and asserting
  // only on the methods used.
  const root = doc as {
    querySelector: (sel: string) => {
      value: string;
      dispatchEvent: (e: unknown) => boolean;
    } | null;
    querySelectorAll: (sel: string) => ArrayLike<{ textContent: string | null; value: string }>;
  };
  const sel = root.querySelector("#login-account");
  if (!sel) throw new Error("未找到账号下拉框");
  const opt = Array.from(root.querySelectorAll("#login-account option")).find(
    (o) => (o.textContent ?? "").trim() === label,
  );
  if (!opt) throw new Error(`未找到账号选项：${label}`);
  sel.value = opt.value;
  sel.dispatchEvent(new testWindow.Event("change", { bubbles: true }));
}

const BASE = {
  "GET /api/auth/parent-accounts": [MOM, DAD],
  "POST /api/auth/login": { token: "jwt-abc", user: { id: 2, display_name: "妈妈" } },
};

const suite: TestSuite = {
  name: "LoginPage（家长端登录）",
  cases: [
    ["渲染账号分组：已设 PIN 可选、未设 PIN 置灰且提示", async () => {
      const r = await mountLogin(BASE);
      await flush();
      assertIncludes(r.text, "家长登录", "登录标题");
      // The not-set group is entirely unselectable: options are disabled with a
      // hint (the optgroup label is an attribute, not in textContent, so assert
      // on the disabled state and the hint text).
      assertIncludes(r.text, "未设 PIN 的账号需先", "提示文案");
      assertIncludes(r.text, "妈妈", "妈妈在列表中");
      assertIncludes(r.text, "爸爸", "爸爸在列表中");
      // Accounts with a PIN set are selectable; the whole not-set group is greyed
      // out and unselectable.
      const opt = Array.from(r.doc.querySelectorAll("#login-account option")).find(
        (o) => (o.textContent ?? "").trim() === "爸爸",
      ) as unknown as { disabled: boolean };
      assert(opt.disabled, "未设 PIN 账号应被置灰不可选");
      const momOpt = Array.from(r.doc.querySelectorAll("#login-account option")).find(
        (o) => (o.textContent ?? "").trim() === "妈妈",
      ) as unknown as { disabled: boolean };
      assertEqual(momOpt.disabled ?? false, false, "已设 PIN 账号可选");
      // It uses the accounts endpoint, not the bare /api/users.
      assert(
        r.requests.some((u) => u.endsWith("/api/auth/parent-accounts")),
        "应请求 GET /api/auth/parent-accounts",
      );
      assert(
        !r.requests.some((u) => u.endsWith("/api/users")),
        "不应再请求 GET /api/users",
      );
    }],

    ["未选中账号时登录按钮禁用", async () => {
      const r = await mountLogin(BASE);
      await flush();
      const btn = findButton(r.doc, "登录") as unknown as { disabled: boolean };
      assertEqual(btn.disabled, true, "无账号无 PIN 时禁用");
    }],

    ["PIN 输入辞掉非数字", async () => {
      const r = await mountLogin(BASE);
      await flush();
      pickAccount(r.doc, "妈妈");
      await flush();
      const pin = r.doc.querySelector("#login-pin") as unknown as { value: string };
      setNativeInput(pin ?? null, "12ab3");
      await flush();
      assertEqual(pin.value, "123", "字母应被辞掉只留数字");
    }],

    ["登录成功：写 token 并按 redirect 回跳", async () => {
      const r = await mountLogin(BASE);
      await flush();
      pickAccount(r.doc, "妈妈");
      setNativeInput(r.doc.querySelector("#login-pin"), "123456");
      await flush();
      clickButton(r.doc, "登录");
      await flush();

      const call = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/auth/login"));
      assert(call, "应 POST /api/auth/login");
      assertEqual(call?.body, JSON.stringify({ user_id: 2, pin: "123456" }), "登录载荷");
      assertEqual(
        testWindow.localStorage.getItem("paperly.parent_token"),
        "jwt-abc",
        "token 已写入 localStorage",
      );
      assertEqual(r.navigations[r.navigations.length - 1] ?? "", "/storage", "按 redirect 回跳原页");
    }],

    ["untrusted redirect 直接回首页（防开放重定向）", async () => {
      const r = await mountLogin(BASE, "/login?redirect=https%3A%2F%2Fevil.example%2F", {});
      await flush();
      pickAccount(r.doc, "妈妈");
      setNativeInput(r.doc.querySelector("#login-pin"), "123456");
      await flush();
      clickButton(r.doc, "登录");
      await flush();
      assertEqual(r.navigations[r.navigations.length - 1] ?? "", "/", "外部地址应回落首页");
    }],

    ["登录凭证错误：展示后端 message 且不写 token", async () => {
      const r = await mountLogin({
        "GET /api/auth/parent-accounts": [MOM],
        "POST /api/auth/login": { code: 40012, message: "账号或 PIN 不正确" },
      });
      await flush();
      pickAccount(r.doc, "妈妈");
      setNativeInput(r.doc.querySelector("#login-pin"), "000000");
      await flush();
      clickButton(r.doc, "登录");
      await flush();
      assertIncludes(r.text, "账号或 PIN 不正确", "错误文案展示");
      assertEqual(
        testWindow.localStorage.getItem("paperly.parent_token"),
        null,
        "失败不应写 token",
      );
    }],
  ],
};

export default suite;