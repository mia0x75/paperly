/**
 * test/home.page.test.ts -- parent home page (Dashboard + system monitor).
 *
 * Covers three things:
 *   1. Normal state: ROW1-4 rendered from kid data; ROW1 top three cards
 *      scoped by "all / one kid" filter;
 *   2. Empty state: with no students a guide hero is shown, and dashboard
 *      is **not** requested;
 *   3. Boundaries: dashboard failure does not crash, system monitor failure
 *      does not drag down the main panels.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage } from "./mount";
import { clickButton } from "./controls";
import HomePage from "../src/pages/HomePage.vue";

/**
 * Response data shaped like the backend GET /api/dashboard (single-scope counts).
 * Field names strictly follow the DTO: exams has the total/this_month/last_month
 * time bases, each a single count -- writing the test wrong would not test the real contract.
 */
const DASHBOARD = {
  exams: {
    total: 42,
    this_month: 7,
    last_month: 5,
  },
  exam_pages: 126,
  series: [
    { date: "2026-09-28", count: 2 },
    { date: "2026-09-29", count: 0 },
    { date: "2026-09-30", count: 4 },
  ],
  distribution: [
    { subject_id: 1, name: "数学", count: 12, percent: 60.0 },
    { subject_id: 2, name: "语文", count: 8, percent: 40.0 },
  ],
  kid: { id: 7, name: "小明", status: 1 },
};

const KIDS = [{ id: 7, name: "小明", grade: 8, status: 1 }];

/** Two kids: to verify the card-level filter can switch to "小红". */
const KIDS_TWO = [
  { id: 7, name: "小明", grade: 8, status: 1 },
  { id: 8, name: "小红", grade: 8, status: 1 },
];

/**
 * Paper-distribution card data (GET /api/dashboard/distribution?kid_id=7, the
 * default smallest-kid scope). Counts differ from DASHBOARD.distribution so the
 * card's independent fetch is distinguishable from the homepage aggregate copy.
 */
const DISTRIBUTION_ALL = {
  items: [
    { subject_id: 1, name: "数学", count: 20, percent: 80.0 },
    { subject_id: 2, name: "语文", count: 5, percent: 20.0 },
  ],
};

/** Data when the distribution card is switched to a kid (kid_id=8). */
const DISTRIBUTION_KID8 = {
  items: [{ subject_id: 1, name: "数学", count: 4, percent: 100.0 }],
};

/**
 * Top-row KPI card counts (GET /api/dashboard/counts, default "all" scope).
 * They match the all values in DASHBOARD (120/30/400), asserting the cards use the all scope.
 */
const SCOPE_COUNTS_ALL = {
  exam_total: 120,
  exam_this_month: 30,
  exam_last_month: 28,
  exam_pages: 400,
};

/** Counts when the top-row KPI card is switched to a kid (kid_id=8). */
const SCOPE_COUNTS_KID8 = {
  exam_total: 3,
  exam_this_month: 1,
  exam_last_month: 0,
  exam_pages: 4,
};

const METRICS = {
  paper: { total: 42, this_month: 7, pending: 3 },
  database: { size_human: "8.0 GB", last_backup_at: "2026-09-29 08:00:00", schema_tag: "v1.0.0" },
  storage: { assets_human: "12.4 MB", disk_used_ratio: 0.75, disk_supported: true },
  health: [
    { key: "database", title: "数据库", status: "ok", value: "8.0 GB", detail: "quick_check 通过" },
    { key: "last_backup", title: "最近备份", status: "warn", value: "从未备份", detail: "建议执行一次" },
  ],
};

const PAPERS = { items: [], next_cursor: 0, has_more: false };

/**
 * Storage-trend (4th-card data source): the second point's db is missing (null) --
 * guards that the "missing sample is not padded to 0" serialization contract
 * survives the page pipeline.
 */
const STORAGE_TREND = {
  days: 30,
  points: [
    { date: "2026-10-06", db_size_kb: 8192, assets_size_kb: 1048576 },
    { date: "2026-10-07", db_size_kb: null, assets_size_kb: 1049600 },
  ],
};

/** Non-empty papers fixture: only a non-empty list exercises the #cell-id render path. */
const PAPERS_NONEMPTY = {
  items: [
    {
      id: 5,
      title: "2026-初一-上学期-数学-10月-月考",
      subject_id: 1,
      grade: 7,
      exam_type: 1,
      exam_date: "2026-10-01",
      kid_id: 7,
      status: 1,
      page_count: 3,
    },
  ],
  next_cursor: 0,
  has_more: false,
};

function responses(
  kids: unknown = KIDS,
  dash: unknown = DASHBOARD,
  metrics: unknown = METRICS,
  papers: unknown = PAPERS,
  dist: unknown = DISTRIBUTION_ALL,
  counts: unknown = SCOPE_COUNTS_ALL,
) {
  return {
    // distribution / counts keys must come before "/api/dashboard": mount's response
    // table matches by substring in order, and "/api/dashboard" is their prefix.
    "/api/dashboard/distribution": dist,
    "/api/dashboard/counts": counts,
    "/api/kids": kids,
    "/api/dashboard": dash,
    "/api/metrics/summary": metrics,
    "/api/metrics/storage-trend": STORAGE_TREND,
    "/api/exams": papers,
    "/api/subjects": [],
  };
}

/**
 * Multi-kid response for "kids' persistence" (GET /api/dashboard/persistence).
 * Two kids each have one window series; kid 7 = currently selected kid (DASHBOARD.kid.id).
 */
const PERSISTENCE = {
  items: [
    {
      kid_id: 7,
      name: "小明",
      series: [
        { date: "2026-09-28", count: 2 },
        { date: "2026-09-29", count: 0 },
        { date: "2026-09-30", count: 4 },
      ],
    },
    {
      kid_id: 8,
      name: "小红",
      series: [
        { date: "2026-09-28", count: 1 },
        { date: "2026-09-29", count: 0 },
        { date: "2026-09-30", count: 0 },
      ],
    },
  ],
};

const suite: TestSuite = {
  name: "HomePage（家长 Dashboard）",
  cases: [
    ["本月新增卡用本月数 + 环比，而非总数", async () => {
      const r = await mountPage(HomePage, { responses: responses() });
      assertIncludes(r.text, "本月新增试卷", "卡2 标题");
      // 30 vs last month 28 → +7%. Using the total 120 as numerator would give +329%.
      assertIncludes(r.text, "较上月 +7%", "环比文案");
      assertNotIncludes(r.text, "329%", "环比分子不能用总数");
      // card 1's subtext is "本月新增 30", not the month-over-month change
      assertIncludes(r.text, "本月新增 30", "卡1 副文案");
    }],
    // Top-row KPI card filter: the first 3 cards each carry their own "all / one kid"
    // dropdown, defaulting to all; switching one card only refetches that card
    // (does not refetch the whole-page aggregate).
    ["家长端 KPI 卡默认「全部」：独立取数、不带 kid_id、各卡都有筛选器", async () => {
      const r = await mountPage(HomePage, { responses: responses(KIDS_TWO) });
      const row1 = r.doc.querySelector("section.grid");
      assert(row1 !== null, "ROW1 应存在");
      // the first three cards each have an "all" filter trigger (the storage card does not).
      const filters = Array.from(row1.querySelectorAll("button")).filter((b) =>
        (b.textContent ?? "").includes("全部"),
      );
      assertEqual(filters.length, 3, "前三张卡各带一个筛选器");
      const countsReq = r.requests.find((u) => u.includes("/api/dashboard/counts"));
      assert(countsReq !== undefined, `应请求计数端点：${r.requests.join(", ")}`);
      assert(!countsReq.includes("kid_id"), `默认「全部」不应带 kid_id：${countsReq}`);
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["家长端 KPI 卡切孩子：只重拉该卡，带 kid_id，数字更新", async () => {
      const r = await mountPage(HomePage, {
        responses: {
          "/api/dashboard/counts?kid_id=8": SCOPE_COUNTS_KID8,
          ...responses(KIDS_TWO),
        },
      });
      const countsBefore = r.requests.filter((u) => u.includes("/api/dashboard/counts")).length;
      const row1 = r.doc.querySelector("section.grid");
      assert(row1 !== null, "ROW1 应存在");
      const card1 = row1.children[0];
      assert(card1 !== undefined, "卡1 应存在");
      const trigger = Array.from(card1.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("全部"),
      );
      assert(trigger !== undefined, "卡1 应有筛选触发器");
      (trigger as unknown as { click: () => void }).click();
      await flush();
      const opt = card1.querySelector('[data-scope-id="8"]');
      assert(opt !== null, "下拉应列出小红（scope-id=8）");
      (opt as unknown as { click: () => void }).click();
      await flush();

      const kidReq = r.requests.find((u) => u.includes("/api/dashboard/counts?kid_id=8"));
      assert(kidReq !== undefined, `切孩子应带 kid_id=8 重拉本卡：${r.requests.join(", ")}`);
      // only this one card is refetched (+1), the other two cards are unaffected
      assertEqual(
        r.requests.filter((u) => u.includes("/api/dashboard/counts")).length,
        countsBefore + 1,
        "切一张卡只应重拉该卡",
      );
      assertIncludes(card1.textContent ?? "", "3", "卡1 应更新为该孩子的总数");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["第 1 行仍是四卡，第 4 卡为存储增长折线图", async () => {
      const r = await mountPage(HomePage, { responses: responses() });
      assertIncludes(r.text, "存储增长", "卡4 标题");
      assertNotIncludes(r.text, "待处理后台任务", "旧任务卡应被替换");
      const row1 = r.doc.querySelector("section.grid");
      assert(row1 !== null, "ROW1 应存在");
      assertEqual(row1.children.length, 4, "第 1 行仍是四卡（不增不减）");
      assert(
        r.requests.some((u) => u.includes("/api/metrics/storage-trend")),
        `第 4 卡应拉取 gauge 历史：${r.requests.join(", ")}`,
      );
      const card4 = row1.children[3];
      assert(card4 !== undefined, "第 4 卡应存在");
      assert(card4.querySelector("canvas") !== null, "第 4 卡应渲染图画布");
      // the legend is a DOM chip row (data-testid="storage-legend"), asserting the
      // chip style is covered by storage-trend-card.test; series assembly by storage-trend.test.
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["存储趋势无数据：空态提示且卡片不塌陷", async () => {
      const r = await mountPage(HomePage, {
        responses: { ...responses(), "/api/metrics/storage-trend": { days: 30, points: [] } },
      });
      assertIncludes(r.text, "暂无存储趋势", "空态标题");
      assertIncludes(r.text, "存储增长", "卡片外壳仍在（不塌陷）");
      assertIncludes(r.text, "归档试卷总数", "其余三卡不受影响");
      assertEqual(r.errors.length, 0, "空态不应抛错");
    }],

    ["存储趋势拉取失败：只坏本卡，不连累主面板", async () => {
      const r = await mountPage(HomePage, {
        responses: {
          ...responses(),
          "/api/metrics/storage-trend": { code: 50001, message: "gauge 读取失败" },
        },
      });
      assertIncludes(r.text, "归档试卷总数", "其余三卡不受影响");
      assertIncludes(r.text, "gauge 读取失败", "本卡展示错误信息");
      assertEqual(r.errors.length, 0, "错误应由卡片处理，不应外溢");
    }],

    ["多孩坚持度：标题变「上传坚持度」并叠加多线", async () => {
      const r = await mountPage(HomePage, {
        // key: in a substring-matching stub, /api/dashboard would catch
        // /api/dashboard/persistence first, so persistence must come first to
        // hit early (mount stubs match in insertion order).
        responses: {
          "/api/dashboard/persistence": PERSISTENCE,
          ...responses(),
        },
      });
      assertIncludes(r.text, "上传坚持度", "多孩模式下标题换集合口径");
      assertNotIncludes(r.text, "小明的坚持度", "不再用单孩子名当标题");
      assertNotIncludes(r.text, "我的坚持度", "家长端不是「我的」");
      assert(
        r.requests.some((u) => u.includes("/api/dashboard/persistence")),
        `应先拉多孩坚持度：${r.requests.join(", ")}`,
      );
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["多孩坚持度：图例随折线一并渲染，两位孩子都有图例", async () => {
      const r = await mountPage(HomePage, {
        responses: {
          "/api/dashboard/persistence": PERSISTENCE,
          ...responses(),
        },
      });
      const legend = r.doc.querySelector('[data-testid="persistence-legend"]');
      assert(legend !== null, "多孩折线下方应有图例");
      assertIncludes(legend!.textContent ?? "", "小明", "图例含第一位孩子名");
      assertIncludes(legend!.textContent ?? "", "小红", "图例含第二位孩子名");
    }],

    ["单个活跃孩子：仍是「上传坚持度」标题，且单线也带图例", async () => {
      // Parent with exactly one active kid: persistence returns one series, the
      // chart enters multi-kid framing but the single line must not lose its key,
      // otherwise the generic title leaves the curve unnamed.
      const SINGLE = { items: [PERSISTENCE.items[0]!] };
      const r = await mountPage(HomePage, {
        responses: {
          "/api/dashboard/persistence": SINGLE,
          ...responses(),
        },
      });
      assertIncludes(r.text, "上传坚持度", "单孩也走多孩口径");
      const legend = r.doc.querySelector('[data-testid="persistence-legend"]');
      assert(legend !== null, "单线下方也应有图例");
      assertIncludes(legend!.textContent ?? "", "小明", "图例仍标识这条曲线");
    }],

    ["多孩数据里没有当前选中孩子：仍是上传坚持度", async () => {
      // parent scope = all active kids, independent of the selected kid: when the
      // selected kid is absent from items (graduated/deleted), other kids' lines
      // still all render and the legend stays complete.
      const OTHER = {
        items: [
          {
            kid_id: 8,
            name: "小红",
            series: [
              { date: "2026-09-28", count: 1 },
              { date: "2026-09-29", count: 0 },
              { date: "2026-09-30", count: 0 },
            ],
          },
        ],
      };
      const r = await mountPage(HomePage, {
        responses: {
          "/api/dashboard/persistence": OTHER,
          ...responses(),
        },
      });
      assertIncludes(r.text, "上传坚持度", "有 persistence 数据就进多孩模式");
      assertNotIncludes(r.text, "小明的坚持度", "不因选中孩子缺席而退回单孩子标题");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["多孩坚持度失败：回退单孩子标题，不报错", async () => {
      const r = await mountPage(HomePage, {
        responses: {
          "/api/dashboard/persistence": { code: 50001, message: "坚持度读取失败" },
          ...responses(),
        },
      });
      assertIncludes(r.text, "小明的坚持度", "失败回退到单孩子折线");
      assertNotIncludes(r.text, "孩子们的坚持度", "拿不到多孩数据就不说谎");
      assertNotIncludes(r.text, "坚持度读取失败", "回退不该把错误上屏");
      const legend = r.doc.querySelector('[data-testid="persistence-legend"]');
      assert(legend !== null, "回退的单孩子折线也应带图例");
      assertIncludes(legend!.textContent ?? "", "我的归档趋势", "图例标识回退曲线的口径");
      assertEqual(r.errors.length, 0, `应静默回退：${r.errors.join(" | ")}`);
    }],

    ["系统监控渲染健康检查行", async () => {
      const r = await mountPage(HomePage, { responses: responses() });
      assertIncludes(r.text, "系统运行监控", "ROW5 标题");
      const monitor = r.doc.querySelector('[data-testid="system-monitor"]');
      assert(monitor !== null, "系统监控区块应存在");
      assertEqual(monitor.querySelectorAll("tbody tr").length, METRICS.health.length, "体检行数");

      // status column is a solid-color dot, not a text label.
      const dots = monitor.querySelectorAll("tbody tr td span[role='img']");
      assertEqual(dots.length, METRICS.health.length, "状态圆点数");
      const dotClasses = Array.from(dots).map((d) => d.className);
      assert(
        dotClasses.some((c) => c.includes("bg-success")),
        `正常状态应有绿点，实际：${dotClasses.join(",")}`,
      );
      assert(
        dotClasses.some((c) => c.includes("bg-warning")),
        `警告状态应有黄点，实际：${dotClasses.join(",")}`,
      );
      // the status text should not be printed into the table either
      assert(!monitor.textContent.includes("ok"), "状态不应显示原始文本 ok");
    }],

    ["没有学生时显示 hero 且不请求 dashboard", async () => {
      const r = await mountPage(HomePage, { responses: responses([], null) });
      assertIncludes(r.text, "还没有学生档案", "空态标题");
      assert(
        !r.requests.some((u) => u.includes("/api/dashboard")),
        `空态不应请求 dashboard，实际：${r.requests.join(", ")}`,
      );
      assert(
        !r.requests.some((u) => u.includes("storage-trend")),
        "空态下 DashboardPanels 未挂载，不应拉存储趋势",
      );
    }],

    ["空态「添加第一个孩子」跳到 /kids（存在 kid-new 死路由的回归）", async () => {
      const r = await mountPage(HomePage, { responses: responses([], null) });
      clickButton(r.doc, "添加第一个孩子");
      await flush();
      assertEqual(r.navigations.at(-1), "/kids", "空态主按钮应跳到孩子列表页");
    }],

    ["dashboard 请求失败时显示错误且不崩", async () => {
      const r = await mountPage(HomePage, {
        responses: {
          ...responses(),
          "/api/dashboard": { code: 40001, message: "试卷不存在" },
          "/api/exams": PAPERS,
        },
      });
      assertIncludes(r.text, "试卷不存在", "错误上屏");
      assertEqual(r.errors.length, 0, "错误应由页面处理");
    }],

    ["系统监控失败不连累主面板", async () => {
      const r = await mountPage(HomePage, {
        responses: {
          "/api/kids": KIDS,
          "/api/dashboard/counts": SCOPE_COUNTS_ALL,
          "/api/dashboard": DASHBOARD,
          "/api/metrics/summary": { code: 50001, message: "体检炸了" },
          "/api/exams": PAPERS,
        },
      });
      // ROW1 should still render
      assertIncludes(r.text, "归档试卷总数", "主面板不受体检失败影响");
      assertNotIncludes(r.text, "体检炸了", "体检错误不该上屏成主错误");
      assertEqual(r.errors.length, 0, "不应有未捕获异常");
    }],

    // Card-level filter: "paper distribution" is always scoped to one kid (default:
    // smallest active id, no "all" option); the top-right can switch to another kid,
    // and only that card is refetched (no top-bar linkup, no whole-page refetch).
    ["家长端试卷分布卡：无「全部」，默认 id 最小孩子，独立取数带 kid_id", async () => {
      const r = await mountPage(HomePage, { responses: responses(KIDS_TWO) });
      assertIncludes(r.text, "试卷分布", "卡片标题");
      const card = r.doc.querySelector('[data-testid="distribution-card"]');
      assert(card !== null, "分布卡应存在");
      assertNotIncludes(card.textContent ?? "", "暂无归档数据", "有数据时不应显示空态");
      // The card is always scoped to one kid (smallest active id = 7), never "all".
      const distReq = r.requests.find((u) => u.includes("/api/dashboard/distribution"));
      assert(distReq !== undefined, `应请求分布端点：${r.requests.join(", ")}`);
      assert(distReq.includes("kid_id=7"), `默认应取 id 最小孩子 7：${distReq}`);
      // No "全部" option; trigger shows the smallest kid's name.
      const trigger = Array.from(card.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("小明"),
      );
      assert(trigger !== undefined, `分布卡触发器应显示 id 最小孩子名：${card.textContent}`);
      (trigger as unknown as { click: () => void }).click();
      await flush();
      assert(card.querySelector('[data-scope-id="all"]') === null, "分布卡下拉不应有「全部」");
      assert(card.querySelector('[data-scope-id="8"]') !== null, "分布卡下拉应列出另一个孩子");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["家长端试卷分布卡切孩子：只重拉本卡，带 kid_id，不重拉整页", async () => {
      const r = await mountPage(HomePage, {
        responses: {
          "/api/dashboard/distribution?kid_id=8": DISTRIBUTION_KID8,
          ...responses(KIDS_TWO),
        },
      });
      const card = r.doc.querySelector('[data-testid="distribution-card"]');
      assert(card !== null, "分布卡应存在");
      const isDashCall = (u: string): boolean =>
        u.includes("/api/dashboard") &&
        !u.includes("/distribution") &&
        !u.includes("/persistence");
      const dashBefore = r.requests.filter(isDashCall).length;
      const trigger = Array.from(card.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("小明"),
      );
      assert(trigger !== undefined, "分布卡应有筛选触发器");
      (trigger as unknown as { click: () => void }).click();
      await flush();
      const opt = card.querySelector('[data-scope-id="8"]');
      assert(opt !== null, "下拉应列出小红（scope-id=8）");
      (opt as unknown as { click: () => void }).click();
      await flush();

      const kidReq = r.requests.find((u) => u.includes("/api/dashboard/distribution?kid_id=8"));
      assert(kidReq !== undefined, `切孩子应带 kid_id=8 重拉本卡：${r.requests.join(", ")}`);
      // the whole-page aggregate must not be refetched: only the card scope is
      // selected, so /api/dashboard calls must not increase.
      assertEqual(
        r.requests.filter(isDashCall).length,
        dashBefore,
        "切孩子不应重拉整页聚合",
      );
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["家长端最新试卷默认全部：不带 kid_id，且卡片内有筛选器", async () => {
      const r = await mountPage(HomePage, { responses: responses(KIDS_TWO) });
      const card = r.doc.querySelector('[data-testid="exam-table-card"]');
      assert(card !== null, "试卷表卡应存在");
      const examReq = r.requests.find((u) => u.includes("/api/exams"));
      assert(examReq !== undefined, "应请求试卷列表");
      assert(!examReq.includes("kid_id"), `默认「全部」不应带 kid_id：${examReq}`);
      const trigger = Array.from(card.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("全部"),
      );
      assert(trigger !== undefined, "试卷表卡应有「全部」筛选触发器");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["ROW4 历史试卷：非空 rows 正常渲染（#cell-id 解构回归）", async () => {
      // Guards a fixed crash: ExamTableCard's #cell-id was written as `#cell-id`
      // instead of `#cell-id="{ row }"`, which throws once rows is non-empty
      // ("Cannot read properties of undefined (reading 'id')"). The fallout was
      // wider than this table -- the exception broke RouterView's update chain,
      // taking down every top-bar navigation.
      const r = await mountPage(HomePage, {
        responses: responses(KIDS, DASHBOARD, METRICS, PAPERS_NONEMPTY),
      });
      assertIncludes(r.text, "2026-初一-上学期-数学-10月-月考", "非空试卷应渲染出标题");
      assertEqual(r.errors.length, 0, `非空试卷不应抛错：${r.errors.join(" | ")}`);
    }],

    ["首页试卷表无「预览」「删除」操作列，标题可点进详情", async () => {
      const r = await mountPage(HomePage, {
        responses: responses(KIDS, DASHBOARD, METRICS, PAPERS_NONEMPTY),
      });
      // Scope to buttons: the status legend legitimately contains the word "删除"
      // as a dot label, so a page-wide text check would false-positive.
      const actionButtons = Array.from(r.doc.querySelectorAll("button")).map(
        (b) => (b.textContent ?? "").trim(),
      );
      assert(!actionButtons.some((t) => t.includes("预览")), "不应再有预览按钮");
      assert(!actionButtons.some((t) => t.includes("删除")), "不应再有删除按钮");
      const link = r.doc.querySelector('a[href="/exams/5"]');
      assert(link !== null, "试卷标题应是指向详情的链接");
      assertIncludes(link.textContent ?? "", "2026-初一-上学期-数学-10月-月考", "链接文案是标题");
    }],

    ["首页最新试卷表常驻「更多」入口，跳到 /exams", async () => {
      const r = await mountPage(HomePage, {
        responses: responses(KIDS, DASHBOARD, METRICS, PAPERS_NONEMPTY),
      });
      clickButton(r.doc, "更多");
      await flush();
      assertEqual(r.navigations.at(-1), "/exams", "「更多」应跳到试卷列表");
    }],

    ["首页最新试卷表首列显示归属孩子名（家长端）", async () => {
      const r = await mountPage(HomePage, {
        responses: responses(KIDS, DASHBOARD, METRICS, PAPERS_NONEMPTY),
      });
      const card = r.doc.querySelector('[data-testid="exam-table-card"]');
      assert(card !== null, "应有最新试卷卡");
      const headers = Array.from(card!.querySelectorAll("thead th")).map(
        (th) => (th.textContent ?? "").trim(),
      );
      assertEqual(headers[0], "归属", `首列应是归属，实际：${headers.join(",")}`);
      assertIncludes(card!.textContent ?? "", "小明", "应显示 kid_id=7 对应的孩子名");
    }],

    ["首页雷达图：下拉无「全部」，默认 id 最小孩子，科目按 ?kid_id 拉取", async () => {
      const r = await mountPage(HomePage, { responses: responses(KIDS_TWO) });

      // Subject axes come from the kid-scoped subject endpoint; default scope is the
      // smallest active kid id (7), never "all".
      const req = r.requests.find((u) => u.includes("/api/subjects?kid_id="));
      assert(req !== undefined, `雷达应请求孩子科目：${r.requests.join(" | ")}`);
      assert(req!.includes("kid_id=7"), `默认应取 id 最小孩子 7：${req}`);

      // The radar dropdown offers no "全部" option, but lists both active kids.
      const radar = r.doc.querySelector('[data-testid="radar-card"]');
      assert(radar !== null, "应有雷达卡");
      const trigger = Array.from(radar!.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("小明"),
      );
      assert(trigger !== undefined, `雷达下拉触发器应显示 id 最小孩子名：${radar!.textContent}`);
      (trigger as { click: () => void }).click();
      await flush();
      assert(radar!.querySelector('[data-scope-id="all"]') === null, "雷达下拉不应有「全部」");
      assert(radar!.querySelector('[data-scope-id="8"]') !== null, "雷达下拉应列出另一个孩子");
    }],

    ["无控制台错误", async () => {
      const r = await mountPage(HomePage, { responses: responses() });
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],
  ],
};

export default suite;
