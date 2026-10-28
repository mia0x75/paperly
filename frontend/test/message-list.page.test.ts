/**
 * test/message-list.page.test.ts -- notification list page (messages table).
 *
 * Covers: row rendering (title / type / time + read-state dot), keyset pagination
 * (next / previous), delete with confirmation, empty state, error state + retry,
 * mark-read on click, deep-link titles, and the parent/kid shared page.
 */
import { assert, assertEqual, assertIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import MessageListPage from "../src/pages/MessageListPage.vue";

const NEWEST = "2026-10-02T10:00:00Z";
const OLDER = "2026-09-30T09:00:00Z";

/** A message row as returned by GET /api/messages (Summary view). */
interface Row {
  id: number;
  kind: number;
  title: string;
  link: string;
  is_read: boolean;
  created_at: string;
}

function row(over: Partial<Row> = {}): Row {
  return {
    id: 12,
    kind: 6,
    title: "备份完成",
    link: "",
    is_read: false,
    created_at: NEWEST,
    ...over,
  };
}

/** A keyset page: next_cursor=0 ⟺ has_more=false. */
function page(items: Row[], nextCursor = 0, hasMore = false) {
  return { items, next_cursor: nextCursor, has_more: hasMore };
}

/**
 * First page: one unread + one read notification, more pages behind.
 *
 * A factory, not a constant: the page writes is_read back into the rows it received
 * (mark-read), and a shared fixture would leak that state into the next case.
 */
function page1() {
  return page(
    [
      row({ id: 12, kind: 6, title: "薄弱点已攻克", is_read: false }),
      row({ id: 11, kind: 7, title: "有 3 道错题待复习", is_read: true, created_at: OLDER }),
    ],
    10,
    true,
  );
}

/** Second page (behind the cursor). */
function page2() {
  return page([row({ id: 10, kind: 99, title: "旧通知", is_read: true, created_at: OLDER })]);
}

/**
 * Mock responses. The keys are matched as URL substrings, first hit wins — so the
 * method-qualified keys (POST/DELETE) must come before the bare list path.
 */
function responses(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "POST /api/messages/read-all": { updated: 1 },
    "POST /api/messages/12/read": null,
    "DELETE /api/messages/12": null,
    "cursor=10": page2(),
    "/api/messages": page1(),
    ...over,
  };
}

async function mountMessages(
  over: Record<string, unknown> = {},
  path = "/messages",
): Promise<MountResult> {
  return mountPage(MessageListPage, { path, responses: responses(over) });
}

/** Click a button whose text contains the label. */
function clickButton(r: MountResult, label: string): void {
  const btn = Array.from(r.doc.body.querySelectorAll("button")).find((b) =>
    (b.textContent ?? "").includes(label),
  );
  assert(btn !== undefined, `未找到按钮：${label}`);
  btn?.dispatchEvent(new r.doc.defaultView!.Event("click", { bubbles: true }));
}

/** The read-state dot of a row (the first dot inside the row's title cell). */
function dotOf(r: MountResult, title: string): string {
  const cell = Array.from(r.doc.body.querySelectorAll("tbody tr")).find((tr) =>
    (tr.textContent ?? "").includes(title),
  );
  assert(cell !== undefined, `未找到通知行：${title}`);
  const dot = cell?.querySelector("span.rounded-full");
  assert(dot !== null && dot !== undefined, `未找到通知圆点：${title}`);
  return dot?.className ?? "";
}

/** All requested /api/messages URLs (list requests only). */
function listRequests(r: MountResult): string[] {
  return r.requests.filter((u) => u.includes("/api/messages?"));
}

const suite: TestSuite = {
  name: "MessageListPage（通知列表 / 分页 / 删除）",
  cases: [
    ["渲染通知行：标题、类型、时间与未读/已读圆点", async () => {
      const r = await mountMessages();
      await flush();

      assertEqual(r.doc.body.querySelectorAll("tbody tr").length, 2, "应渲染两行通知");
      assertIncludes(r.text, "薄弱点已攻克", "通知标题");
      assertIncludes(r.text, "有 3 道错题待复习", "通知标题");
      assertIncludes(r.text, "薄弱点攻克", "类型中文映射");
      assert(
        dotOf(r, "薄弱点已攻克").includes("bg-destructive"),
        "未读应为实色圆点",
      );
      assert(
        dotOf(r, "有 3 道错题待复习").includes("bg-muted-foreground/40"),
        "已读应为浅灰圆点",
      );
    }],

    ["首屏按页大小取数，不带游标", async () => {
      const r = await mountMessages();
      await flush();

      assertEqual(listRequests(r).length, 1, "首屏只请求一次");
      assert(listRequests(r)[0]?.includes("limit=10") === true, "应带 limit=10");
      assert(listRequests(r)[0]?.includes("cursor=") !== true, "首屏不带游标");
    }],

    ["「下一页」带游标，「上一页」退回首页", async () => {
      const r = await mountMessages();
      await flush();

      clickButton(r, "下一页");
      await flush();

      const paged = listRequests(r);
      assertEqual(paged.length, 2, "应发两次列表请求");
      assert(paged[1]?.includes("cursor=10") === true, `第二页应带游标，实际 ${paged[1]}`);
      assertIncludes(r.text, "旧通知", "应渲染第二页内容");

      clickButton(r, "上一页");
      await flush();

      const back = listRequests(r);
      assertEqual(back.length, 3, "回上一页应再请求一次");
      assert(back[2]?.includes("cursor=") !== true, "回到首页不带游标");
      assertIncludes(r.text, "薄弱点已攻克", "应回到第一页内容");
    }],

    ["没有更多时「下一页」禁用", async () => {
      const r = await mountMessages({ "/api/messages": page2() });
      await flush();

      const next = Array.from(r.doc.body.querySelectorAll("button")).find(
        (b) => (b.textContent ?? "").includes("下一页"),
      );
      assert((next as unknown as { disabled?: boolean })?.disabled === true, "无更多时下一页应禁用");
    }],

    ["删除要确认：确认后发 DELETE 并重载列表", async () => {
      const live = page([row({ id: 12, title: "备份完成" }), row({ id: 11, title: "旧通知" })]);
      const map = responses({ "/api/messages": live });
      const r = await mountPage(MessageListPage, { path: "/messages", responses: map });
      await flush();

      // Click the row's delete icon -> the confirmation dialog appears, nothing is sent yet.
      const del = r.doc.body.querySelector('button[aria-label="删除通知：备份完成"]');
      assert(del !== null, "应有删除按钮");
      del?.dispatchEvent(new r.doc.defaultView!.Event("click", { bubbles: true }));
      await flush();
      assertIncludes(r.text, "确定删除", "应弹出确认框");
      assert(
        !r.calls.some((c) => c.method === "DELETE"),
        "确认前不得发送删除请求",
      );

      // Confirm -> DELETE + reload; the server-side list no longer has that row.
      map["/api/messages"] = page(live.items.filter((m) => m.id !== 12));
      clickButton(r, "确认删除");
      await flush();

      const delCall = r.calls.find((c) => c.method === "DELETE" && c.url.includes("/api/messages/12"));
      assert(delCall !== undefined, `应 DELETE /api/messages/12，实际 ${JSON.stringify(r.calls)}`);
      assert(!r.text.includes("备份完成"), "删除后该行应消失");
      assertIncludes(r.text, "旧通知", "其他行应保留");
    }],

    ["空列表显示「暂无通知」而非空表", async () => {
      const r = await mountMessages({ "/api/messages": page([]) });
      await flush();

      assertIncludes(r.text, "暂无通知", "空态文案");
      assertEqual(r.doc.body.querySelectorAll("tbody tr").length, 0, "空态不渲染数据行");
    }],

    ["请求失败显示错误并可重试", async () => {
      const r = await mountMessages({ "/api/messages": { code: 50001, message: "通知读取失败" } });
      await flush();

      assertIncludes(r.text, "通知读取失败", "错误上屏");
      assertEqual(r.errors.length, 0, "错误不应冒泡成未捕获异常");

      clickButton(r, "重试");
      await flush();
      assertEqual(listRequests(r).length, 2, "重试应重新请求");
    }],

    ["点未读通知标题：标记已读，圆点转灰", async () => {
      const r = await mountMessages();
      await flush();

      clickButton(r, "薄弱点已攻克");
      await flush();

      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/messages/12/read"));
      assert(post !== undefined, `应 POST /api/messages/12/read，实际 ${JSON.stringify(r.calls)}`);
      assert(
        dotOf(r, "薄弱点已攻克").includes("bg-muted-foreground/40"),
        "已读后圆点应变浅灰",
      );
    }],

    ["已读通知点标题不再发请求", async () => {
      const r = await mountMessages();
      await flush();
      const before = r.calls.length;

      clickButton(r, "有 3 道错题待复习");
      await flush();

      assertEqual(r.calls.length, before, "已读项不应再发 markRead");
    }],

    ["带 link 的标题是深链：点击跳转并标记已读", async () => {
      const r = await mountMessages({
        "/api/messages": page([row({ id: 12, title: "已自动升级", link: "/kids/7" })]),
      });
      await flush();

      const link = Array.from(r.doc.body.querySelectorAll("a")).find((a) =>
        (a.textContent ?? "").includes("已自动升级"),
      );
      assert(link !== undefined, "带 link 的标题应渲染为链接");
      link?.dispatchEvent(new r.doc.defaultView!.Event("click", { bubbles: true }));
      await flush();

      assert(r.navigations.includes("/kids/7"), `应跳转 /kids/7，实际 ${r.navigations.join(",")}`);
      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/messages/12/read"));
      assert(post !== undefined, "点击深链也应标记已读");
    }],

    ["「全部已读」按钮：有未读才出现，点击 POST read-all", async () => {
      const r = await mountMessages();
      await flush();
      assertIncludes(r.text, "全部已读", `有未读时应出现「全部已读」，实际：${r.text.slice(0, 400)}`);

      clickButton(r, "全部已读");
      await flush();

      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/messages/read-all"));
      assert(post !== undefined, "应 POST /api/messages/read-all");
    }],

    ["全部已读时不再显示「全部已读」按钮", async () => {
      const r = await mountMessages({
        "/api/messages": page([row({ id: 11, title: "旧通知", is_read: true })]),
      });
      await flush();
      assert(!r.text.includes("全部已读"), "没有未读时不该出现「全部已读」");
    }],

    ["孩子端 /kid/messages 共用同一页面组件", async () => {
      const r = await mountMessages({}, "/kid/messages");
      await flush();

      assertEqual(r.doc.body.querySelectorAll("tbody tr").length, 2, "孩子端也应渲染通知");
      assertIncludes(r.text, "薄弱点已攻克", "孩子端也应显示通知标题");
    }],
  ],
};

export default suite;
