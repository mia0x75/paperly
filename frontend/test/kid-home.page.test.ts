/**
 * test/kid-home.page.test.ts -- kid home page (/kid, JWT-driven).
 *
 * The point is not "it renders too" but "the data source switched correctly":
 *   - GET /api/kid/dashboard (requireKid, kid_id taken from JWT) + GET /api/me;
 *   - never calls /api/kid-access/*, which doesn't exist and would 404 to a
 *     blank screen.
 * It is also isomorphic to the parent side: same DashboardPanels, and the counts
 * are single-scope, so the top row shows the kid's own numbers only.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { mountPage } from "./mount";
import KidHomePage from "../src/pages/KidHomePage.vue";
import { KID_TOKEN_KEY } from "../src/lib/kidSession";

/** Kid-side response: single-scope counts for this kid, no global denominator anywhere. */
const DASHBOARD = {
  exams: {
    total: 18,
    this_month: 3,
    last_month: 2,
  },
  exam_pages: 54,
  series: [{ date: "2026-09-30", count: 2 }],
  distribution: [{ subject_id: 1, name: "数学", count: 6, percent: 100.0 }],
  kid: { id: 7, name: "小明", status: 1 },
};

/** GET /api/me kid audience (MeKid). */
const ME = { id: 7, name: "小明", grade: 9 };

/** Storage trend: same card on kid and parent sides (requireAny audience), data shaped the same. */
const STORAGE_TREND = {
  days: 30,
  points: [{ date: "2026-10-07", db_size_kb: 8192, assets_size_kb: 1048576 }],
};

/** JWT-session mount options: the session lives in localStorage (token), not in the URL. */
function session(responses: Record<string, unknown>): {
  path: string;
  responses: Record<string, unknown>;
  storage: Record<string, string>;
} {
  return { path: "/kid", responses, storage: { [KID_TOKEN_KEY]: "jwt-kid" } };
}

const suite: TestSuite = {
  name: "KidHomePage（学生 Dashboard）",
  cases: [
    ["孩子端不触发家长分布端点、无卡片级筛选", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      assert(
        !r.requests.some((u) => u.includes("/api/dashboard/distribution")),
        `孩子端不应请求家长分布端点：${r.requests.join(", ")}`,
      );
      assert(
        !r.requests.some((u) => u.includes("/api/dashboard/counts")),
        `孩子端不应请求家长计数端点：${r.requests.join(", ")}`,
      );
      // the kid side renders SubjectDonut directly (no DistributionCard), so it
      // has no "all" filter.
      const card = r.doc.querySelector('[data-testid="distribution-card"]');
      assert(card === null, "孩子端分布卡不应走家长端 DistributionCard");
      assertIncludes(r.text, "我的试卷分布", "孩子端仍是 kid 收窄的分布");
    }],

    ["孩子端首行 KPI 只出单项计数、无卡片级筛选", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      const row1 = r.doc.querySelector("section.grid");
      assert(row1 !== null, "ROW1 应存在");
      // the kid side has no self-fetch count cards; must not call the parent count endpoint
      assert(
        !r.requests.some((u) => u.includes("/api/dashboard/counts")),
        `孩子端不应请求计数端点：${r.requests.join(", ")}`,
      );
      // the first three cards carry no "all / one kid" filter
      const filters = Array.from(row1.querySelectorAll("button")).filter((b) =>
        (b.textContent ?? "").includes("全部"),
      );
      assertEqual(filters.length, 0, "孩子端 KPI 卡不应带筛选器");
      // the single number comes from the dashboard's kid count (18), no "of" denominator
      assertIncludes(r.text, "18", "卡1 显示孩子自己的总数");
      assert(!r.text.includes(" of "), `不应再有 of 分母：${r.text}`);
    }],

    ["孩子端保留「我的试卷分布」", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      assertIncludes(r.text, "我的试卷分布", "孩子端也要有试卷分布");
    }],

    ["孩子端保留薄弱知识点雷达图占位", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      assertIncludes(r.text, "雷达图", "雷达图占位要留着");
      assertIncludes(r.text, "预留", "占位要写明是预留（与加载失败区分）");
    }],

    ["孩子端首页显示「我的最新试卷」主表（行链接走 /kid/exams）", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({
          "/api/kid/dashboard": DASHBOARD,
          "/api/me": ME,
          "/api/exams": { items: [{ id: 1, title: "期中卷", kid_id: 7 }], next_cursor: 0, has_more: false },
          "/api/subjects": [{ id: 1, name: "数学" }],
        }),
      });
      assertIncludes(r.text, "我的最新试卷", "表格标题要在");
      // the ownership column is parent-side only: the kid side is locked to its own
      // papers, so there must be no ownership column.
      const card = r.doc.querySelector('[data-testid="exam-table-card"]');
      assert(card !== null, "应有最新试卷卡");
      const headers = Array.from(card!.querySelectorAll("thead th")).map(
        (th) => (th.textContent ?? "").trim(),
      );
      assert(!headers.includes("归属"), `孩子端不应有归属列，实际：${headers.join(",")}`);
      // row links must point to the kid-side route /kid/exams/:id, not the parent /exams/:id.
      const link = r.doc.body.querySelector('a[href="/kid/exams/1"]');
      assert(link !== null, "行链接应指向 /kid/exams/:id");
      assertNotIncludes(
        r.text,
        "历史试卷",
        "标题文案沿用「我的最新试卷」，不该出现旧称",
      );
    }],

    ["JWT 驱动：请求 /api/kid/dashboard 与 /api/me", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      assert(
        r.requests.some((u) => u.includes("/api/kid/dashboard")),
        `应请求 /api/kid/dashboard：${r.requests.join(", ")}`,
      );
      assert(
        r.requests.some((u) => u.includes("/api/me")),
        `应请求 /api/me：${r.requests.join(", ")}`,
      );
    }],

    ["会话档案写入顶栏用的名字与列表用的 kid_id", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      assertEqual(
        (globalThis as Record<string, unknown>).localStorage === undefined
          ? null
          : localStorage.getItem("paperly.kid_name"),
        "小明",
        "顶栏名由 /api/me 写入会话",
      );
      assertEqual(localStorage.getItem("paperly.kid_id"), "7", "列表过滤用的 kid_id 落存储");
      assert(r.errors.length === 0, `控制台错误：${r.errors.join(" | ")}`);
    }],

    ["孩子端首页同样渲染存储增长卡（双端要求）", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({
          "/api/kid/dashboard": DASHBOARD,
          // storage-trend must come before "/api/me": mount's response table
          // matches by substring in order, and "/api/me" is a prefix of
          // "/api/metrics/..."; otherwise the trend request would grab MeKid as
          // data and the card silently becomes empty.
          "/api/metrics/storage-trend": STORAGE_TREND,
          "/api/me": ME,
        }),
      });
      assertIncludes(r.text, "存储增长", "第 4 卡对孩子端同样渲染");
      const row1 = r.doc.querySelector("section.grid");
      assert(row1 !== null, "ROW1 应存在");
      assertEqual(row1.children.length, 4, "孩子端第 1 行也是四卡");
      assert(row1.querySelector("canvas") !== null, "折线图画布");
      assert(
        r.requests.some((u) => u.includes("/api/metrics/storage-trend")),
        "应拉取 gauge 历史",
      );
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["学生端没有系统运行监控", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      assertNotIncludes(r.text, "系统运行监控", "系统运行监控是家长专属");
    }],

    // the only exception to the blocking rule is /metrics/storage-trend: the storage
    // growth card shows on both parent and kid sides (gauge is single-machine
    // scale, not sensitive to kids).
    ["家长端独有的 metrics 不被请求（storage-trend 例外）", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      const opsOnly = r.requests.filter(
        (u) => u.includes("/api/metrics") && !u.includes("/api/metrics/storage-trend"),
      );
      assert(opsOnly.length === 0, `学生端不应请求运维接口：${opsOnly.join(", ")}`);
      assert(
        r.requests.some((u) => u.includes("/api/metrics/storage-trend")),
        "存储增长是双端共享卡，孩子端应请求其历史序列",
      );
    }],

    ["后端报错（如 40100 会话失效）显示错误且不崩", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({
          "/api/kid/dashboard": { code: 40100, message: "登录已过期" },
          "/api/me": { code: 40100, message: "登录已过期" },
        }),
      });
      assertIncludes(r.text, "登录已过期", "错误上屏");
      assertEqual(r.errors.length, 0, "不冒泡成未捕获异常");
    }],

    ["无控制台错误", async () => {
      const r = await mountPage(KidHomePage, {
        ...session({ "/api/kid/dashboard": DASHBOARD, "/api/me": ME }),
      });
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],
  ],
};

export default suite;
