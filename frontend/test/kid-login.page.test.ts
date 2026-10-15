/**
 * test/kid-login.page.test.ts -- kid login page (/kid/login).
 *
 * Covers: the account list comes from /api/auth/kid-accounts; an empty list shows
 * actionable text and sends no login; a successful login writes the kid token and
 * navigates to /kid; 40100 -> "PIN wrong", 42900 -> "too many attempts"; a list
 * load failure goes to ErrorState + retry; PIN input rejects non-digits.
 *
 * Key boundary: 40100 (wrong PIN) does **not** clear the token or navigate --
 * that's a credential validation error, not an expired session (held by
 * client.ts's isCredentialPath exemption).
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { clickButton, findButton, setNativeInput } from "./controls";
import { testWindow } from "./setup";
import { KID_TOKEN_KEY } from "../src/lib/kidSession";
import KidLoginPage from "../src/pages/KidLoginPage.vue";

const XIAOMING = { id: 7, name: "小明" };
const XIAOHONG = { id: 8, name: "小红" };

async function mountKidLogin(responses: Record<string, unknown>): Promise<MountResult> {
  return mountPage(KidLoginPage, { path: "/kid/login", responses });
}

/**
 * Select an option in the kid dropdown (native select, driven by a change event
 * for v-model, same as LoginPage).
 */
function pickKid(doc: unknown, name: string): void {
  const root = doc as {
    querySelector: (sel: string) => {
      value: string;
      dispatchEvent: (e: unknown) => boolean;
    } | null;
    querySelectorAll: (sel: string) => ArrayLike<{ textContent: string | null; value: string }>;
  };
  const sel = root.querySelector("#kid-login-select");
  if (!sel) throw new Error("未找到孩子下拉框");
  const opt = Array.from(root.querySelectorAll("#kid-login-select option")).find(
    (o) => (o.textContent ?? "").trim() === name,
  );
  if (!opt) throw new Error(`未找到孩子选项：${name}`);
  sel.value = opt.value;
  sel.dispatchEvent(new testWindow.Event("change", { bubbles: true }));
}

const OK = {
  "GET /api/auth/kid-accounts": [XIAOMING, XIAOHONG],
  "POST /api/kid/auth/login": { token: "jwt-kid", kid: { id: 7, name: "小明" } },
};

function token(): string | null {
  return testWindow.localStorage.getItem(KID_TOKEN_KEY);
}

const suite: TestSuite = {
  name: "KidLoginPage（孩子登录）",
  cases: [
    ["渲染孩子列表：来自 /api/auth/kid-accounts，未选人时按钮禁用", async () => {
      const r = await mountKidLogin(OK);
      await flush();
      assertIncludes(r.text, "孩子登录", "标题");
      assertIncludes(r.text, "小明", "列表含小明");
      assertIncludes(r.text, "小红", "列表含小红");
      assert(
        r.requests.some((u) => u.includes("/api/auth/kid-accounts")),
        `应请求 kid-accounts：${r.requests.join(", ")}`,
      );
      assert(
        !r.requests.some((u) => u.includes("/api/users")),
        "不得回退到家长账号接口",
      );
      const btn = findButton(r.doc, "登录") as unknown as { disabled: boolean };
      assertEqual(btn.disabled, true, "未选人未输 PIN 时禁用");
    }],

    ["空列表：「暂无孩子账号」文案，不渲染表单、不发登录", async () => {
      const r = await mountKidLogin({
        "GET /api/auth/kid-accounts": [],
        "POST /api/kid/auth/login": { token: "x" },
      });
      await flush();
      assertIncludes(r.text, "暂无孩子账号，请家长先邀请", "空态给可行动文案");
      assertEqual(r.doc.querySelector("#kid-login-select"), null, "无账号时不出表单");
      assert(
        !r.requests.some((u) => u.includes("/api/kid/auth/login")),
        "空账号不发注定失败的登录请求",
      );
    }],

    ["登录成功：写孩子 token 并跳 /kid，载荷为 {user_id, pin}", async () => {
      const r = await mountKidLogin(OK);
      await flush();
      pickKid(r.doc, "小明");
      setNativeInput(r.doc.querySelector("#kid-login-pin"), "123456");
      await flush();
      clickButton(r.doc, "登录");
      await flush();
      const login = r.calls.find((c) => c.url.includes("/api/kid/auth/login"));
      assert(login !== undefined, "应发起登录");
      assertEqual(
        login?.body ?? "",
        JSON.stringify({ user_id: 7, pin: "123456" }),
        "登录载荷（后端合同字段是 user_id）",
      );
      assertEqual(token(), "jwt-kid", "成功后写入孩子 token");
      assert(r.navigations.includes("/kid"), `应跳 /kid：${r.navigations.join(" → ")}`);
    }],

    ["40100：「PIN 错误」文案，不清 token、不跳转", async () => {
      const r = await mountKidLogin({
        "GET /api/auth/kid-accounts": [XIAOMING],
        "POST /api/kid/auth/login": { code: 40100, message: "账号或 PIN 不正确" },
      });
      await flush();
      pickKid(r.doc, "小明");
      setNativeInput(r.doc.querySelector("#kid-login-pin"), "1111");
      await flush();
      clickButton(r.doc, "登录");
      await flush();
      assertIncludes(r.text, "PIN 错误", "按业务码收口的文案");
      assertEqual(token(), null, "PIN 错不写会话 token");
      assert(!r.navigations.includes("/kid"), "PIN 错不得跳转");
      const btn = findButton(r.doc, "登录") as unknown as { disabled: boolean };
      assertEqual(btn.disabled, false, "失败后应可重试");
    }],

    ["42900：「尝试过多」文案上屏", async () => {
      const r = await mountKidLogin({
        "GET /api/auth/kid-accounts": [XIAOMING],
        "POST /api/kid/auth/login": { code: 42900, message: "尝试次数过多，请稍后" },
      });
      await flush();
      pickKid(r.doc, "小明");
      setNativeInput(r.doc.querySelector("#kid-login-pin"), "1111");
      await flush();
      clickButton(r.doc, "登录");
      await flush();
      assertIncludes(r.text, "尝试过多", "限流文案（spec 要求含「尝试过多」）");
      assertEqual(token(), null, "限流不写 token");
    }],

    ["PIN 输入辞掉非数字", async () => {
      const r = await mountKidLogin(OK);
      await flush();
      pickKid(r.doc, "小明");
      const pin = r.doc.querySelector("#kid-login-pin") as unknown as { value: string };
      setNativeInput(pin, "12ab34");
      await flush();
      assertEqual(pin.value, "1234", "字母被辞掉只留数字");
    }],

    ["列表加载失败：ErrorState + 重试", async () => {
      const r = await mountKidLogin({
        "GET /api/auth/kid-accounts": { code: 50001, message: "服务不可用" },
      });
      await flush();
      assertIncludes(r.text, "服务不可用", "错误文案透传");
      clickButton(r.doc, "重试");
      await flush();
      const lists = r.requests.filter((u) => u.includes("/api/auth/kid-accounts"));
      assertEqual(lists.length, 2, "重试应再拉一次列表");
    }],

    ["无导航壳：登录前不渲染任何顶栏", async () => {
      const r = await mountKidLogin(OK);
      await flush();
      assertEqual(r.doc.body.querySelectorAll("nav").length, 0, "无 nav");
      assertEqual(r.doc.body.querySelectorAll("header").length, 0, "无 header");
      assertNotIncludes(r.text, "试卷库", "不带家长端导航");
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],
  ],
};

export default suite;
