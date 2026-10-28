/**
 * test/message-bell.test.ts -- top-bar bell (MessageBell) behavior.
 *
 * Covers fetch boundaries and interaction semantics:
 *   - mounting only fetches the unread count, not the whole notification list
 *     (the bell is on every page);
 *   - the list is only fetched on first expand, and asks for at most BELL_LIMIT (6);
 *   - clicking a notification = mark read only: the row stays in place and its dot
 *     turns gray (no jump, no removal);
 *   - more than six notifications -> "全部通知" jumps to the full list page;
 *   - "mark all read" only affects the parent-side recipient boundary;
 *   - empty state / load failure do not crash, and a failure can be retried.
 */
import { assert, assertEqual, assertIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { h } from "vue";
import Layout from "../src/layouts/Layout.vue";
import MessageBell from "../src/components/MessageBell.vue";

const NEWEST = "2026-10-02T10:00:00Z";

/** The kid-side bell: Layout passes the kid list path through the allTo prop. */
const ChildBell = { render: () => h(MessageBell, { allTo: "/kid/messages" }) };

/**
 * Same shape as GET /api/messages: keyset cursor, no total.
 *
 * A factory, not a constant: clicking a notification writes is_read back into the
 * rows it received, and a shared fixture would leak that state into the next case.
 */
function messages() {
  return {
    items: [
      { id: 12, kind: 6, title: "备份完成", is_read: false, created_at: NEWEST },
      { id: 11, kind: 5, title: "有 3 道错题待复习", is_read: false, created_at: "2026-10-01T09:00:00Z" },
      { id: 10, kind: 7, title: "备份失败", is_read: true, created_at: "2026-09-30T09:00:00Z" },
    ],
    next_cursor: 0,
    has_more: false,
  };
}

function responses(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "/api/messages/unread-count": { unread_count: 2 },
    "/api/messages": messages(),
    ...over,
  };
}

async function openBell(r: MountResult): Promise<void> {
  const trigger = r.doc.body.querySelector('button[title$="通知"]');
  assert(trigger !== null, "应能找到铃铛触发器");
  trigger?.dispatchEvent(new r.doc.defaultView!.Event("click", { bubbles: true }));
  await flush();
}

function clickText(r: MountResult, text: string): void {
  const btn = Array.from(r.doc.body.querySelectorAll("button")).find((b) =>
    (b.textContent ?? "").includes(text),
  );
  assert(btn !== undefined, `应能找到按钮「${text}」`);
  btn?.dispatchEvent(new r.doc.defaultView!.Event("click", { bubbles: true }));
}

const suite: TestSuite = {
  name: "MessageBell（通知铃铛）",
  cases: [
    ["挂载只拉未读数，不拉通知列表", async () => {
      const r = await mountPage(MessageBell, { router: false, responses: responses() });
      assertEqual(r.requests.length, 1, `应只有一个请求，实际：${r.requests.join(", ")}`);
      assert(r.requests[0]?.includes("unread-count") === true, "第一个请求应是未读数");
    }],

    ["未读数为 0 无红点，>0 显示红点", async () => {
      const zero = await mountPage(MessageBell, {
        router: false,
        responses: responses({ "/api/messages/unread-count": { unread_count: 0 } }),
      });
      assertEqual(zero.doc.body.querySelector('[data-testid="unread-badge"]'), null, "0 不该有红点");

      const two = await mountPage(MessageBell, { router: false, responses: responses() });
      assert(two.doc.body.querySelector('[data-testid="unread-badge"]') !== null, "有未读应有红点");
    }],

    ["首次展开才拉列表，并渲染标题与类型", async () => {
      const r = await mountPage(MessageBell, { router: false, responses: responses() });
      await openBell(r);

      assert(r.requests.some((u) => u.includes("/api/messages?")), "展开应拉列表");
      assertIncludes(r.text, "备份完成", "通知标题");
      assertIncludes(r.text, "备份", "类型中文映射");
    }],

    ["下拉只取最新 6 条（limit=6）", async () => {
      const r = await mountPage(MessageBell, { router: false, responses: responses() });
      await openBell(r);

      const listCall = r.calls.find((c) => c.method === "GET" && c.url.includes("/api/messages?"));
      assert(listCall !== undefined, "展开应发一次列表请求");
      assert(
        listCall?.url.includes("limit=6") === true,
        `下拉只应取最新 6 条，实际 URL：${listCall?.url}`,
      );
    }],

    ["点某条通知：POST read + 条目保留 + 圆点转灰 + 不跳转", async () => {
      const r = await mountPage(MessageBell, { path: "/messages", responses: responses() });
      await openBell(r);

      clickText(r, "有 3 道错题待复习");
      await flush();

      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/11/read"));
      assert(post !== undefined, `应 POST /api/messages/11/read，实际：${JSON.stringify(r.calls)}`);
      assertIncludes(r.text, "有 3 道错题待复习", "已读通知应留在下拉里，不移除");
      assert(
        r.doc.body.querySelector('[data-testid="unread-badge"]') !== null,
        "还剩 1 条未读，红点应仍存在",
      );
      assertEqual(r.navigations.length, 0, "点击通知不跳转（深链在列表页）");

      // The clicked row's dot flips to the light-gray "read" color.
      const row = Array.from(r.doc.body.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("有 3 道错题待复习"),
      );
      const dot = row?.querySelector("span.rounded-full");
      assert(dot !== null && dot !== undefined, "应能找到该条通知的圆点");
      assert(
        (dot?.className ?? "").includes("bg-muted-foreground/40"),
        `已读圆点应为浅灰，实际类名：${dot?.className}`,
      );
    }],

    ["已读通知点了不再发请求", async () => {
      const r = await mountPage(MessageBell, { router: false, responses: responses() });
      await openBell(r);
      const before = r.calls.length;

      clickText(r, "备份失败");
      await flush();

      assertEqual(r.calls.length, before, "已读项不应再发 markRead");
    }],

    ["「全部已读」POST read-all 并把列表置为已读", async () => {
      const r = await mountPage(MessageBell, { router: false, responses: responses() });
      await openBell(r);

      clickText(r, "全部已读");
      await flush();

      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/messages/read-all"));
      assert(post !== undefined, "应 POST /api/messages/read-all");
      assertEqual(r.doc.body.querySelector('[data-testid="unread-badge"]'), null, "角标应清零");
    }],

    ["空态显示引导文案而非空白", async () => {
      const r = await mountPage(MessageBell, {
        router: false,
        responses: responses({ "/api/messages": { items: [], next_cursor: 0, has_more: false } }),
      });
      await openBell(r);
      assertIncludes(r.text, "暂无通知", "空态标题");
    }],

    ["列表请求失败显示错误与重试，重试后可恢复", async () => {
      const r = await mountPage(MessageBell, {
        router: false,
        responses: responses({ "/api/messages": { code: 50001, message: "通知炸了" } }),
      });
      await openBell(r);
      assertIncludes(r.text, "通知炸了", "错误上屏");
      assertEqual(r.errors.length, 0, "错误应由组件处理而非抛到全局");

      clickText(r, "重试");
      await flush();
      assert(!r.text.includes("通知炸了"), "重试后应脱离错误态");
    }],

    ["has_more 时给出「全部通知」入口并跳转到列表页", async () => {
      const r = await mountPage(MessageBell, {
        path: "/messages",
        responses: responses({
          "/api/messages": { items: messages().items, next_cursor: 10, has_more: true },
        }),
      });
      await openBell(r);
      assertIncludes(r.text, "全部通知", "还有更多时应给出完整列表入口");
      assert(!r.text.includes("加载更多"), "下拉不再内联翻页");

      clickText(r, "全部通知");
      await flush();

      assert(
        r.navigations.includes("/messages"),
        `应跳转到 /messages，实际：${JSON.stringify(r.navigations)}`,
      );
      // The dropdown is a glance: it must not pull a second page in place.
      const paged = r.calls.filter((c) => c.method === "GET" && c.url.includes("/api/messages?"));
      assertEqual(paged.length, 1, "下拉只应请求一次列表");
    }],

    ["「全部通知」入口走 allTo（孩子端指向 /kid/messages）", async () => {
      const child = await mountPage(ChildBell, {
        path: "/kid/messages",
        responses: responses({
          "/api/messages": { items: messages().items, next_cursor: 10, has_more: true },
        }),
      });
      await openBell(child);
      clickText(child, "全部通知");
      await flush();
      assert(
        child.navigations.includes("/kid/messages"),
        `孩子端应跳转 /kid/messages，实际：${JSON.stringify(child.navigations)}`,
      );
    }],

    ["铃铛触发器与相邻「设置」按钮同一套配色（BUG 回归）", async () => {
      // The bell trigger must use the same classes as the settings trigger (both are
      // DropdownMenus). A mismatched text-foreground token made the bell one shade
      // brighter than its neighbours in both dark and light mode.
      const r = await mountPage(Layout, {
        responses: {
          "/api/kids": [{ id: 7, name: "小明", grade: 8, status: 1 }],
          "/api/messages/unread-count": { unread_count: 0 },
        },
      });
      await flush();
      const bell = r.doc.body.querySelector('button[title="通知"]');
      const settings = r.doc.body.querySelector('button[title="系统设置"]');
      assert(bell !== null && settings !== null, "顶栏应同时渲染铃铛与设置触发器");
      assertEqual(bell!.className, settings!.className, "铃铛触发器类名必须与设置触发器逐类一致");
      assertIncludes(bell!.className ?? "", "text-muted-foreground", "铃铛图标色应与邻居一致");
      assert(!(bell!.className ?? "").includes("text-control-foreground"), "不再残留 text-control-foreground");
    }],
  ],
};

export default suite;