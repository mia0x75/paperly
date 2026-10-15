/**
 * test/welcome.page.test.ts -- first-run onboarding page (/welcome).
 *
 * Covers: the three-step flow (welcome -> parent -> kid), parent registration
 * (empty state -> add -> list refresh), "next"/"finish" disabled by entity count,
 * and on completion POST /onboarding/complete then navigate to "/".
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { findButton, clickButton, setNativeInput } from "./controls";
import { testWindow } from "./setup";
import type { Document } from "happy-dom";
import WelcomePage from "../src/pages/WelcomePage.vue";

const MOM = { id: 2, display_name: "妈妈", pin_set: false, status: 1 };
const KID = { id: 7, name: "小明", grade: 7, status: 1 };

interface WelcomeMount {
  r: MountResult;
  /**
   * Overridable response table: the stub re-reads the Live object on every call,
   * so changing a key swaps the response.
   */
  responses: Record<string, unknown>;
}

async function mountWelcome(userCount: number): Promise<WelcomeMount> {
  const users: unknown[] = [];
  for (let i = 1; i <= userCount; i++) users.push(MOM);
  const responses = {
    "GET /api/users": users,
    "GET /api/kids": [KID],
    "POST /api/users": MOM,
    "POST /api/onboarding/complete": { done: true },
  };
  const r = await mountPage(WelcomePage, {
    path: "/welcome",
    responses,
  });
  return { r, responses };
}

/**
 * Asserts a button's disabled state (happy-dom button node; only an explicit
 * disabled attribute counts).
 */
function assertDisabled(doc: Document, label: string, disabled: boolean): void {
  const btn = findButton(doc, label) as unknown as { disabled: boolean };
  assertEqual(btn.disabled, disabled, `按钮「${label}」禁用态`);
}

const suite: TestSuite = {
  name: "WelcomePage（首启引导）",
  cases: [
    ["第一步展示欢迎文案与步骤说明", async () => {
      const { r } = await mountWelcome(1);
      assertIncludes(r.text, "欢迎使用《拾卷》", "欢迎标题");
      assertIncludes(r.text, "开始设置", "进入按钮");
      assertIncludes(r.text, "登记家长账号", "步骤说明");
      assertNotIncludes(r.text, "上一步", "第一步无上一步");
    }],

    ["第二步：任一家长存在时「下一步」可用，进入第三步", async () => {
      const { r } = await mountWelcome(1);
      clickButton(r.doc, "开始设置");
      await flush();
      assertIncludes(r.text, "登记家长账号", "第二步标题");
      assertIncludes(r.text, "妈妈", "家长列表渲染");
      assertDisabled(r.doc, "下一步", false);
      clickButton(r.doc, "下一步");
      await flush();
      assertIncludes(r.text, "登记孩子", "进入第三步");
    }],

    ["第二步：暂无家长时空态 + 「下一步」禁用", async () => {
      const { r } = await mountWelcome(0);
      clickButton(r.doc, "开始设置");
      await flush();
      assertIncludes(r.text, "还没有家长账号", "空态提示");
      assertDisabled(r.doc, "下一步", true);
    }],

    ["登记家长：填昵称提交 → POST /api/users → 列表刷新并可继续", async () => {
      const { r, responses } = await mountWelcome(0);
      clickButton(r.doc, "开始设置");
      await flush();

      clickButton(r.doc, "添加家长");
      await flush();
      // Type a nickname in the modal, then submit "register".
      const nameInput = r.doc.querySelector('input[placeholder*="妈妈"]');
      assert(nameInput, "昵称输入框已渲染");
      setNativeInput(nameInput, "妈妈");
      clickButton(r.doc, "登记");
      // After submitting, simulate the server having created the user: swap the
      // GET response to a new reference. The stub reads the Live object per call;
      // if every GET returned the same reference, the ref assignment wouldn't
      // retrigger recompute and the computed would stay stale. The real backend
      // returns a fresh object each time, so a new array matches the contract.
      responses["GET /api/users"] = [MOM];
      await flush();

      const createCall = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/users"));
      assert(createCall, "已 POST /api/users");
      assertEqual(createCall?.body, JSON.stringify({ display_name: "妈妈", pin: undefined }), "提交载荷");
      assertIncludes(r.text, "妈妈", "刷新后列表出现新家长");
      assertDisabled(r.doc, "下一步", false);
    }],

    ["完成后 POST /api/onboarding/complete 并跳转首页", async () => {
      const { r } = await mountWelcome(1);
      clickButton(r.doc, "开始设置");
      await flush();
      clickButton(r.doc, "下一步");
      await flush();
      assertIncludes(r.text, "小明", "孩子列表渲染");

      clickButton(r.doc, "完成引导");
      await flush();
      const completeCall = r.calls.find((c) => c.method === "POST" && c.url.includes("/onboarding/complete"));
      assert(completeCall, "已 POST /api/onboarding/complete");
      assertEqual(r.navigations[r.navigations.length - 1] ?? "", "/", "完成后跳到首页");
    }],

    ["完成后写入家长 token（P2 方案 A）", async () => {
      const { r, responses } = await mountWelcome(1);
      // Plan A: complete returns the default parent JWT; the frontend writes it to
      // localStorage and enters the dashboard directly.
      responses["POST /api/onboarding/complete"] = {
        done: true,
        token: "jwt-onboarding",
        user: { id: 2, display_name: "妈妈" },
      };
      clickButton(r.doc, "开始设置");
      await flush();
      clickButton(r.doc, "下一步");
      await flush();
      clickButton(r.doc, "完成引导");
      await flush();
      assertEqual(
        testWindow.localStorage.getItem("paperly.parent_token"),
        "jwt-onboarding",
        "complete 返回 token 时应写入",
      );
    }],

    ["complete 未返回 token 时不写（无在册家长的兜底）", async () => {
      const { r, responses } = await mountWelcome(1);
      responses["POST /api/onboarding/complete"] = { done: true };
      clickButton(r.doc, "开始设置");
      await flush();
      clickButton(r.doc, "下一步");
      await flush();
      clickButton(r.doc, "完成引导");
      await flush();
      assertEqual(
        testWindow.localStorage.getItem("paperly.parent_token"),
        null,
        "无 token 返回时不该写",
      );
    }],

    ["完成按钮在无孩子时禁用", async () => {
      const r = await mountPage(WelcomePage, {
        path: "/welcome",
        responses: {
          "GET /api/users": [MOM],
          "GET /api/kids": [],
          "POST /api/onboarding/complete": { done: true },
        },
      });
      clickButton(r.doc, "开始设置");
      await flush();
      clickButton(r.doc, "下一步");
      await flush();
      assertIncludes(r.text, "还没有孩子", "空态提示");
      assertDisabled(r.doc, "完成引导", true);
    }],
  ],
};

/** Keeps the testWindow import used at the type level; cases go through mountPage's stub. */
void testWindow;

export default suite;
