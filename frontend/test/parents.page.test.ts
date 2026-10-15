/**
 * test/parents.page.test.ts -- parent management sub-page (/parents).
 *
 * Covers (scoped decision: add only + list + own rename / own PIN change):
 *   the local-machine marker only lands on one's own row; other rows are read-only;
 *   rename goes through PromptModal -> POST /api/me/rename;
 *   PIN change goes through PinChangeModal -> POST /api/me/pin, and the returned
 *   new token is written back;
 *   when setting a PIN for the first time, the payload carries no old PIN.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { findButton, clickButton, setNativeInput } from "./controls";
import { testWindow } from "./setup";
import { PARENT_TOKEN_KEY } from "../src/lib/parentSession";
import ParentsPage from "../src/pages/settings/ParentsPage.vue";

const MOM = { id: 1, display_name: "妈妈", pin_set: true, status: 1 };
const DAD = { id: 2, display_name: "爸爸", pin_set: false, status: 1 };

async function mountParents(
  responses: Record<string, unknown>,
  me = { id: 1, display_name: "妈妈" },
): Promise<MountResult> {
  const base: Record<string, unknown> = {
    "GET /api/me": me,
    "GET /api/users": [MOM, DAD],
    ...responses,
  };
  return mountPage(ParentsPage, {
    path: "/parents",
    responses: base,
    storage: { "paperly.parent_token": "jwt-old" },
  });
}

const suite: TestSuite = {
  name: "ParentsPage（家长管理）",
  cases: [
    ["渲染家长列表，本机标识只落在本人行", async () => {
      const r = await mountParents({});
      await flush();
      assertIncludes(r.text, "家长管理", "页面标题");
      assertIncludes(r.text, "妈妈", "本人行");
      assertIncludes(r.text, "爸爸", "他人行");
      assertIncludes(r.text, "已设 PIN", "妈妈 PIN 状态");
      assertIncludes(r.text, "未设 PIN", "爸爸 PIN 状态");
      // The "this machine" badge must appear exactly once (on the mom's row).
      const badges = Array.from(r.doc.querySelectorAll("span")).filter((s) =>
        (s.textContent ?? "").includes("本机"),
      );
      assertEqual(badges.length, 1, "本机徽标唯一");
      const dadLi = Array.from(r.doc.querySelectorAll("li")).find((li) =>
        (li.textContent ?? "").includes("爸爸"),
      );
      assert(dadLi !== undefined, "爸爸行存在");
      const dadText = dadLi?.textContent ?? "";
      assertNotIncludes(dadText, "改名", "他人行不该有改名按钮");
      assertNotIncludes(dadText, "改 PIN", "他人行不该有改 PIN 按钮");
      assertIncludes(dadText, "仅本人可改", "他人行应提示仅本人可改");
      // One's own row has both rename and PIN-change entries.
      const momLi = Array.from(r.doc.querySelectorAll("li")).find((li) =>
        (li.textContent ?? "").includes("妈妈"),
      );
      assert(momLi !== undefined, "妈妈行存在");
      assertIncludes(momLi?.textContent ?? "", "改名", "本人行有改名");
      assertIncludes(momLi?.textContent ?? "", "改 PIN", "本人行有改 PIN");
    }],

    ["改 PIN：提交新 PIN 并回写新 token", async () => {
      const r = await mountParents({
        "POST /api/me/pin": { token: "jwt-new" },
      });
      await flush();
      clickButton(r.doc, "改 PIN");
      await flush();
      assertIncludes(r.text, "修改入场 PIN", "改 PIN 弹层");
      // PIN already set -> requireOld=true, the "current PIN" input appears.
      const oldInput = r.doc.querySelector('input[placeholder*="当前 PIN"]');
      assert(oldInput !== undefined, "应显示当前 PIN 输入框");
      setNativeInput(oldInput, "1234");
      const newInput = r.doc.querySelector('input[placeholder*="4-6 位数字"]');
      assert(newInput !== undefined, "应显示新 PIN 输入框");
      setNativeInput(newInput, "5678");
      await flush();
      clickButton(r.doc, "保存");
      await flush();

      const call = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/me/pin"));
      assert(call !== undefined, "应 POST /api/me/pin");
      assertEqual(
        JSON.parse(call?.body ?? "{}").new_pin,
        "5678",
        "提交新 PIN",
      );
      assertEqual(
        testWindow.localStorage.getItem(PARENT_TOKEN_KEY),
        "jwt-new",
        "后端返回的新 JWT 应写回换 token",
      );
    }],

    ["首次设 PIN：不出现当前 PIN 框，负载 old_pin 为空", async () => {
      // The local user is dad (id=2), who has no PIN set.
      const r = await mountParents(
        { "POST /api/me/pin": { token: "jwt-dad" } },
        { id: 2, display_name: "爸爸" },
      );
      await flush();
      clickButton(r.doc, "设 PIN");
      await flush();
      assert(
        r.doc.querySelector('input[placeholder*="当前 PIN"]') === null,
        "首次设 PIN 不应出现当前 PIN 输入框",
      );
      const newInput = r.doc.querySelector('input[placeholder*="4-6 位数字"]');
      assert(newInput !== undefined, "应显示新 PIN 输入框");
      setNativeInput(newInput, "8888");
      await flush();
      clickButton(r.doc, "保存");
      await flush();

      const call = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/me/pin"));
      assert(call !== undefined, "应 POST /api/me/pin");
      const body = JSON.parse(call?.body ?? "{}") as { old_pin?: string; new_pin: string };
      assertEqual(body.old_pin ?? "", "", "首设时 old_pin 应为空串");
      assertEqual(body.new_pin, "8888", "提交新 PIN");
    }],

    ["改名：PromptModal 提交新昵称", async () => {
      const r = await mountParents({
        "POST /api/me/rename": { id: 1, display_name: "妈妈新" },
      });
      await flush();
      clickButton(r.doc, "改名");
      await flush();
      // PromptModal's single text input is the nickname field.
      const nameInput = r.doc.querySelector('input[type="text"]');
      assert(nameInput !== undefined, "改名弹层应有昵称输入框");
      setNativeInput(nameInput, "妈妈新");
      await flush();
      clickButton(r.doc, "保存");
      await flush();

      const call = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/me/rename"));
      assert(call !== undefined, "应 POST /api/me/rename");
      assertEqual(
        JSON.parse(call?.body ?? "{}").display_name,
        "妈妈新",
        "提交新昵称",
      );
    }],
  ],
};

export default suite;