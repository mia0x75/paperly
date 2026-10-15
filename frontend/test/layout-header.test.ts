/**
 * test/layout-header.test.ts -- top bar (Layout) behavior.
 *
 * Covers: all four actions present (exams / quick add exam / bell / settings),
 * theme switching, and the bell badge fetch boundary (mounting only pulls the
 * unread count, not the whole notification list; a failure does not break the bar).
 *
 * A quick-add-exam entry replaces the old student switcher.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage } from "./mount";
import Layout from "../src/layouts/Layout.vue";

const RESPONSES = {
  "/api/messages/unread-count": { unread_count: 2 },
  "/api/messages": { items: [], next_cursor: 0, has_more: false },
};

const suite: TestSuite = {
  name: "Layout（顶栏）",
  cases: [
    ["顶栏四项齐备：快速添加试卷 / 试卷 / 铃铛 / 设置", async () => {
      const r = await mountPage(Layout, { responses: RESPONSES });
      // The quick-add-exam entry comes before the exams entry.
      assertIncludes(r.doc.body.innerHTML, "/exams/upload", "应有快速添加试卷入口");
      const addIdx = r.doc.body.innerHTML.indexOf("/exams/upload");
      const examsIdx = r.doc.body.innerHTML.indexOf('href="/exams"');
      assert(addIdx !== -1 && examsIdx !== -1, "两个试卷入口都应存在");
      assert(
        addIdx < examsIdx,
        `快速添加试卷应排在试卷入口之前（add=${addIdx}, exams=${examsIdx}）`,
      );
      assertIncludes(r.doc.body.innerHTML, "/exams", "应有试卷入口");
      const bell = r.doc.body.querySelector('button[title$="通知"]');
      assert(bell !== null, "铃铛按钮的 title 应表达未读数");
      assertIncludes(r.doc.body.innerHTML, "系统设置", "应有齿轮设置入口");
      const settingsTriggers = Array.from(r.doc.body.querySelectorAll('button[title="系统设置"]'));
      assertEqual(settingsTriggers.length, 1, "设置入口应是弹出菜单触发器，而非链接");
      assertNotIncludes(r.doc.body.innerHTML, "/settings", "/settings 已彻底移除，顶栏不应再指向它");
      assertEqual(r.requests.filter((u) => u.includes("/api/messages/unread-count")).length, 1,
        "角标只该拉一次未读数");
    }],

    ["主题切换留在顶栏，且可切换", async () => {
      const r = await mountPage(Layout, { responses: RESPONSES });
      const btn = r.doc.body.querySelector('button[title="切换到亮色"]');
      assert(btn !== null, "顶栏应有主题切换按钮（暗色下提示切到亮色）");

      btn?.dispatchEvent(new r.doc.defaultView!.Event("click", { bubbles: true }));
      await flush();
      assertEqual(
        r.doc.body.querySelector('button[title="切换到暗色"]') !== null,
        true,
        "点一次应切到亮色，按钮文案随之反转",
      );
      // Switch back so later cases see the default theme.
      r.doc.body
        .querySelector('button[title="切换到暗色"]')
        ?.dispatchEvent(new r.doc.defaultView!.Event("click", { bubbles: true }));
      await flush();
    }],

    ["铃铛角标显示未读数，且不为角标拉整页通知", async () => {
      const r = await mountPage(Layout, { responses: RESPONSES });
      const badge = r.doc.body.querySelector('[data-testid="unread-badge"]');
      assert(badge !== null, "存在未读时应显示红点");
      assertEqual(badge?.textContent?.trim(), "", "红点不带数字");
      assert(!r.requests.some((u) => u.includes("/api/messages?")), "挂载时不应拉通知列表");
    }],

    ["未读数为 0 时不渲染角标", async () => {
      const r = await mountPage(Layout, {
        responses: { ...RESPONSES, "/api/messages/unread-count": { unread_count: 0 } },
      });
      assertEqual(r.doc.body.querySelector('[data-testid="unread-badge"]'), null, "不该有空角标");
    }],

    ["角标请求失败不崩，顶栏照常渲染", async () => {
      const r = await mountPage(Layout, {
        responses: {
          ...RESPONSES,
          "/api/messages/unread-count": { code: 50001, message: "boom" },
        },
      });
      assertIncludes(r.doc.body.innerHTML, "/exams", "角标失败不应连累顶栏");
      assertEqual(r.doc.body.querySelector('[data-testid="unread-badge"]'), null, "失败时无角标");
    }],

    // Footer: brand on two lines on the left, one render-time line on the right (no backend call).
    ["页脚左侧品牌两行 + 右侧渲染耗时一行", async () => {
      const r = await mountPage(Layout, { responses: RESPONSES });
      await flush();
      assertIncludes(r.text, "Powered by Paperly.", "品牌行");
      assertIncludes(r.text, "Scan, Score, Succeed.", "标语行");
      assertIncludes(r.text, "本次渲染", "渲染耗时");
      assertNotIncludes(r.text, "goroutine", "不展示后端进程快照");
      assertNotIncludes(r.text, "GC ", "不展示后端 GC");
    }],
  ],
};

export default suite;
