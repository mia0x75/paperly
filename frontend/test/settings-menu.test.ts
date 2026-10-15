/**
 * test/settings-menu.test.ts -- top-bar "settings" popup menu.
 *
 * The menu is pure navigation: each of the 8 items jumps to a top-level route and
 * there are no form controls.
 * Closing behaviour (click outside the panel / Esc / click a menu item) comes from
 * DropdownMenu; each item is pinned here.
 */
import { assert, assertEqual, assertIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import Layout from "../src/layouts/Layout.vue";
// happy-dom has its own type system, structurally different from lib.dom's Element/Event;
// the happy-dom types are used uniformly here (see the same note in test/mount.ts).
import type { Element as HappyElement, Event as HappyEvent, KeyboardEvent as HappyKeyEvent } from "happy-dom";

/** Matches the requirements list: label -> top-level route. */
const MENU: Array<{ label: string; path: string }> = [
  { label: "存储健康与冷备份", path: "/storage" },
  { label: "图片压缩", path: "/image" },
  { label: "孩子与免密授权", path: "/kids-auth" },
  { label: "家长管理", path: "/parents" },
  { label: "异步与后台队列", path: "/tasks" },
  { label: "科目管理", path: "/subjects" },
  { label: "知识点管理", path: "/knowledge-points" },
  { label: "系统版本迁移与任务游标", path: "/system" },
];

const RESPONSES = {
  "/api/kids": [{ id: 7, name: "小明", grade: 8, status: 1 }],
  "/api/messages/unread-count": { unread_count: 0 },
};

async function mountWithMenu(): Promise<MountResult> {
  const r = await mountPage(Layout, { responses: RESPONSES });
  await flush();
  openMenu(r);
  await flush();
  return r;
}

/** Locate the settings trigger by title (the bar may hold several DropdownMenus). */
function settingsTrigger(r: MountResult): HappyElement {
  const btn = r.doc.body.querySelector('button[title="系统设置"]');
  if (btn === null) throw new Error("顶栏应有「系统设置」触发器");
  return btn;
}

function click(r: MountResult, el: HappyElement | null): void {
  el?.dispatchEvent(new (r.doc.defaultView!.Event)("click", { bubbles: true }));
}

function openMenu(r: MountResult): void {
  click(r, settingsTrigger(r));
}

function menuItems(r: MountResult): HappyElement[] {
  return Array.from(r.doc.body.querySelectorAll('[role="menuitem"]'));
}

const suite: TestSuite = {
  name: "SettingsMenu（设置弹出菜单）",
  cases: [
    ["顶栏渲染设置菜单触发器（图标按钮，非链接）", async () => {
      const r = await mountPage(Layout, { responses: RESPONSES });
      const trigger = settingsTrigger(r);
      assertEqual(trigger.tagName, "BUTTON", "触发器应是按钮");
      assertEqual(trigger.getAttribute("aria-expanded"), "false", "初始未展开");
      assertEqual(r.doc.body.querySelectorAll('[role="menuitem"]').length, 0, "初始不渲染菜单项");
    }],

    ["点触发器弹出菜单，再点收起", async () => {
      const r = await mountPage(Layout, { responses: RESPONSES });
      await flush();
      openMenu(r);
      await flush();
      assertEqual(settingsTrigger(r).getAttribute("aria-expanded"), "true", "点开应展开");
      assert(menuItems(r).length > 0, "展开后应有菜单项");

      openMenu(r);
      await flush();
      assertEqual(settingsTrigger(r).getAttribute("aria-expanded"), "false", "再点应收起");
    }],

    ["菜单恰好 8 项，文案与顺序与需求清单一致", async () => {
      const r = await mountWithMenu();
      // Settings menu items are located by data-menu-path (the bell overlay has no
      // such marker).
      const settings = Array.from(r.doc.body.querySelectorAll("[data-menu-path]"));
      assertEqual(settings.length, 8, `菜单应恰好 8 项，实际 ${settings.length}`);
      settings.forEach((el, i) => {
        const want = MENU[i];
        assert(want !== undefined, `清单缺第 ${i + 1} 项`);
        assertIncludes(el.textContent ?? "", want.label, `第 ${i + 1} 项文案`);
        assertEqual(el.getAttribute("data-menu-path"), want.path, `第 ${i + 1} 项落点`);
      });
    }],

    ["菜单里没有任何表单控件（导航，不是容器）", async () => {
      const r = await mountWithMenu();
      const panel = r.doc.body.querySelector('[role="menu"]');
      assert(panel !== null, "应有 role=menu 的浮层面板");
      assertEqual(panel!.querySelectorAll("input").length, 0, "菜单不应有 input");
      assertEqual(panel!.querySelectorAll("select").length, 0, "菜单不应有 select");
      assertEqual(panel!.querySelectorAll("textarea").length, 0, "菜单不应有 textarea");
      assertEqual(panel!.querySelectorAll('button[type="submit"]').length, 0, "菜单不应有提交按钮");
    }],

    ["点第 N 项跳到对应顶层路由，且菜单随之关闭", async () => {
      for (const [i, item] of MENU.entries()) {
        const r = await mountWithMenu();
        const target = r.doc.body.querySelectorAll("[data-menu-path]")[i];
        assert(target !== undefined, `第 ${i + 1} 项应存在`);
        (target as unknown as { click: () => void }).click();
        await flush();
        assertIncludes(r.navigations.join(","), item.path, `第 ${i + 1} 项应跳 ${item.path}`);
        assertEqual(
          settingsTrigger(r).getAttribute("aria-expanded"),
          "false",
          `第 ${i + 1} 项点完菜单应关闭`,
        );
      }
    }],

    ["点击浮层外关闭", async () => {
      const r = await mountWithMenu();
      assertEqual(settingsTrigger(r).getAttribute("aria-expanded"), "true", "先展开");
      const backdrop = r.doc.body.querySelector("div.fixed.inset-0");
      assert(backdrop !== null, "应有覆盖层承接「点外面关闭」");
      click(r, backdrop);
      await flush();
      assertEqual(settingsTrigger(r).getAttribute("aria-expanded"), "false", "点外面应收起");
      assertEqual(r.doc.body.querySelectorAll("[data-menu-path]").length, 0, "收起后菜单项应卸载");
    }],

    ["按 Esc 关闭", async () => {
      const r = await mountWithMenu();
      assertEqual(settingsTrigger(r).getAttribute("aria-expanded"), "true", "先展开");
      settingsTrigger(r).dispatchEvent(
        new (r.doc.defaultView!.KeyboardEvent)("keydown", { key: "Esc", bubbles: true }),
      );
      await flush();
      assertEqual(settingsTrigger(r).getAttribute("aria-expanded"), "false", "Esc 应收起");
    }],

  ],
};

export default suite;