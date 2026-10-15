/**
 * test/kid-topbar.test.ts -- kid top bar (Layout variant="kid").
 *
 * Parent and kid sides share layouts/Layout.vue (KidLayout is just its shell),
 * and the top bar switches views by variant. Both branches live in the same
 * component, so regression must watch both.
 *
 * kid variant: no student switcher, settings menu shrinks to 2 items (subjects /
 * knowledge points) pointing at /kid/*, the papers entry carries its own
 * accessCode, bell / theme / Logo / Footer stay.
 * parent variant: 8-item menu.
 */
import {
  assert,
  assertEqual,
  assertIncludes,
  assertNotIncludes,
  type TestSuite,
} from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
// Mount the KidLayout / ParentLayout shells rather than Layout itself: variant is
// a prop passed by the shell, mountPage can't pass props, and going through the
// shell also covers "which route table wraps which shell -> which view's top bar".
import KidLayout from "../src/layouts/KidLayout.vue";
import ParentLayout from "../src/layouts/ParentLayout.vue";
import { KID_NAME_KEY } from "../src/lib/kidSession";
import type { Element as HappyElement } from "happy-dom";

const RESPONSES = {
  "/api/kids": [{ id: 7, name: "小明", grade: 8, status: 1 }],
  "/api/messages/unread-count": { unread_count: 0 },
};

/**
 * The kid variant needs a path carrying :accessCode, otherwise the top bar gets no
 * accessCode (the real route is /kid/:accessCode/...). Here the harness's
 * parameterized route is used directly.
 */
async function mountKid(path = "/kid/abc123"): Promise<MountResult> {
  return mountPage(KidLayout, { responses: RESPONSES, path });
}

function click(r: MountResult, el: HappyElement | null): void {
  el?.dispatchEvent(new (r.doc.defaultView!.Event)("click", { bubbles: true }));
}

function settingsTrigger(r: MountResult): HappyElement {
  const btn = r.doc.body.querySelector('button[title="系统设置"]');
  if (btn === null) throw new Error("顶栏应有「系统设置」触发器");
  return btn;
}

function menuItems(r: MountResult): HappyElement[] {
  return Array.from(r.doc.body.querySelectorAll('[role="menuitem"]'));
}

const suite: TestSuite = {
  name: "Layout kid 变体（孩子端顶栏）",
  cases: [
    ["kid 变体不去拉孩子列表（省一次 /api/kids，也不怕未授权报错）", async () => {
      const r = await mountKid();
      assertEqual(
        r.requests.filter((u) => u.includes("/api/kids")).length,
        0,
        "kid 变体不该请求 /api/kids",
      );
    }],

    ["kid 变体仍渲染设置入口（图标按钮，与家长端同一触发器规格）", async () => {
      const r = await mountKid();
      const trigger = settingsTrigger(r);
      assertEqual(trigger.tagName, "BUTTON", "应是按钮而非链接");
      assertIncludes(r.doc.body.innerHTML, "系统设置", "设置入口图标在");
    }],

    ["kid 变体设置菜单只有 2 项：科目管理、知识点管理", async () => {
      const r = await mountKid();
      click(r, settingsTrigger(r));
      await flush();
      const items = menuItems(r);
      assertEqual(items.length, 2, `孩子端只应有 2 项菜单，实际 ${items.length}`);
      assertEqual(items[0]?.getAttribute("data-menu-path"), "/kid/subjects", "第 1 项是科目管理");
      assertEqual(
        items[1]?.getAttribute("data-menu-path"),
        "/kid/knowledge-points",
        "第 2 项是知识点管理",
      );
    }],

    ["kid 菜单不含家长端另外 6 项", async () => {
      const r = await mountKid();
      click(r, settingsTrigger(r));
      await flush();
      for (const label of [
        "存储健康与冷备份",
        "图片压缩",
        "孩子与免密授权",
        "家长管理",
        "异步与后台队列",
        "系统版本迁移与任务游标",
      ]) {
        assertNotIncludes(r.text, label, `孩子端菜单不应有「${label}」`);
      }
      assertNotIncludes(r.doc.body.innerHTML, "/storage", "孩子端菜单不应指向 /storage");
    }],

    ["kid 菜单点击跳 /kid/subjects 与 /kid/knowledge-points", async () => {
      const r = await mountKid();
      click(r, settingsTrigger(r));
      await flush();
      const first = menuItems(r)[0];
      if (!first) throw new Error("菜单应有科目管理项");
      click(r, first);
      await flush();
      assertEqual(r.navigations.at(-1), "/kid/subjects", "点科目管理应跳 /kid/subjects");
    }],

    ["kid 菜单知识点项跳 /kid/knowledge-points", async () => {
      const r = await mountKid();
      click(r, settingsTrigger(r));
      await flush();
      const second = menuItems(r)[1];
      if (!second) throw new Error("菜单应有知识点管理项");
      click(r, second);
      await flush();
      assertEqual(
        r.navigations.at(-1),
        "/kid/knowledge-points",
        "点知识点管理应跳 /kid/knowledge-points",
      );
    }],

    ["kid 菜单里无表单控件", async () => {
      const r = await mountKid();
      click(r, settingsTrigger(r));
      await flush();
      assertEqual(r.doc.body.querySelectorAll('[role="menuitem"] input').length, 0, "无 input");
      assertEqual(r.doc.body.querySelectorAll('[role="menuitem"] select').length, 0, "无 select");
    }],

    ["kid 顶栏保留 Logo / 铃铛 / 主题切换", async () => {
      const r = await mountKid();
      assertIncludes(r.doc.body.innerHTML, "Paperly", "Logo 保留");
      const bell = r.doc.body.querySelector('button[title$="通知"]');
      assert(bell !== null, "铃铛保留");
      const theme = r.doc.body.querySelector('button[title*="切换到"]');
      assert(theme !== null, "主题切换保留");
    }],

    ["kid 试卷入口是干净的 /kid/exams（不拼 accessCode）", async () => {
      const r = await mountKid("/kid/exams");
      const link = r.doc.body.querySelector('a[title="试卷"]');
      assert(link !== null, "试卷入口应存在");
      // The entry captures accessCode into localStorage; child routes are always
      // clean paths -- the code is a credential and must not leak with every
      // share / screenshot / history record.
      assertEqual(link?.getAttribute("href"), "/kid/exams", "试卷入口应是字面量 /kid/exams");
    }],

    ["kid 顶栏有「添加试卷」入口，指向干净的 /kid/exams/upload", async () => {
      const r = await mountKid("/kid/exams");
      const link = r.doc.body.querySelector('a[title="添加试卷"]');
      assert(link !== null, "孩子端顶栏应有添加试卷入口");
      assertEqual(
        link?.getAttribute("href"),
        "/kid/exams/upload",
        "添加试卷入口应是字面量 /kid/exams/upload",
      );
    }],

    ["无会话也仍显示试卷入口（顶栏不替页面藏入口）", async () => {
      // Literal routes like /kid/subjects carry no accessCode and no stored code.
      // Hiding the entry on "no accessCode" removed the papers button from every new
      // kid page, leaving the kid side without paper management. Session-expiry
      // notices are reported by the page; the top bar just draws the entry.
      const r = await mountPage(KidLayout, { responses: RESPONSES, path: "/kid/subjects" });
      const link = r.doc.body.querySelector('a[title="试卷"]');
      assertEqual(
        link?.getAttribute("href"),
        "/kid/exams",
        "无会话也应给出试卷入口",
      );
      assert(settingsTrigger(r) !== null, "设置入口仍应在");
    }],

    ["kid 点 Logo 回 /kid，不掉进家长首页", async () => {
      // Logo must point at the kid home, not the parent view.
      const r = await mountKid("/kid/subjects");
      const logo = r.doc.body.querySelector('a.font-title');
      assertEqual(logo?.getAttribute("href"), "/kid", "Logo 应指向孩子首页");
    }],

    ["kid 顶栏在 Logo 后常驻「{名字}的学习空间」（任意孩子页面）", async () => {
      // Home fetches the dashboard and stores the name; the top bar shows it
      // reactively, and sub-pages like /kid/subjects read it from localStorage
      // after refresh.
      const r = await mountPage(KidLayout, {
        responses: RESPONSES,
        path: "/kid/subjects",
        storage: { [KID_NAME_KEY]: "小明" },
      });
      const logo = r.doc.body.querySelector('a.font-title');
      assertEqual(logo?.getAttribute("href"), "/kid", "Logo 仍指向孩子首页");
      // The name sits next to the Logo on the same row, not as a standalone line.
      assertIncludes(r.text, "小明的学习空间", "顶栏应常驻「名字的学习空间」");
      const parent = logo?.parentElement;
      assert(
        parent !== null && parent !== undefined && parent.textContent?.includes("小明的学习空间"),
        "名字应在 Logo 的同一容器内",
      );
    }],

    ["kid 顶栏名字缺失时（无会话）不显示学习空间文案", async () => {
      const r = await mountPage(KidLayout, { responses: RESPONSES, path: "/kid/subjects" });
      assertNotIncludes(r.text, "学习空间", "没查过 dashboard 就不该凭空报名字");
    }],

    ["parent 变体（回归）Logo 仍指 /", async () => {
      const r = await mountPage(ParentLayout, { responses: RESPONSES });
      const logo = r.doc.body.querySelector('a.font-title');
      assertEqual(logo?.getAttribute("href"), "/", "家长端 Logo 行为不变");
    }],

    // ---- parent regression: the kid branch must not skew the parent side ----
    ["parent 变体（回归）设置菜单仍是 8 项", async () => {
      const r = await mountPage(ParentLayout, { responses: RESPONSES });
      await flush();
      click(r, settingsTrigger(r));
      await flush();
      const items = menuItems(r);
      assertEqual(items.length, 8, `家长端菜单应仍是 8 项，实际 ${items.length}`);
      assertEqual(items[5]?.getAttribute("data-menu-path"), "/subjects", "第 6 项仍是 /subjects");
      assertEqual(
        items[6]?.getAttribute("data-menu-path"),
        "/knowledge-points",
        "第 7 项仍是 /knowledge-points",
      );
    }],

    ["parent 变体（回归）试卷入口仍是 /exams", async () => {
      const r = await mountPage(ParentLayout, { responses: RESPONSES });
      const link = r.doc.body.querySelector('a[title="试卷"]');
      assertEqual(link?.getAttribute("href"), "/exams", "家长端试卷入口不变");
    }],
  ],
};

export default suite;