/**
 * test/settings.page.test.ts -- the 7 standalone settings pages
 * (/storage /image /kids-auth /parents ...).
 *
 * Covers: each sub-page fetches from real endpoints and renders, adding a subject
 * goes through a modal, system-provided subjects cannot be deleted, the storage
 * page's "back up now" hits POST /api/admin/backup, deleted kids can be restored,
 * and the compression preset is echoed and saved.
 * Modal interactions (selectors/dates) are handled uniformly by test/controls.ts.
 *
 * The parents list/self-management page (/parents) is asserted in parents.page.test.ts.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { setNativeInput } from "./controls";
import StoragePage from "../src/pages/settings/StoragePage.vue";
import ImageSettingsPage from "../src/pages/settings/ImageSettingsPage.vue";
import KidsAuthPage from "../src/pages/settings/KidsAuthPage.vue";
import TasksPage from "../src/pages/settings/TasksPage.vue";
import SubjectsPage from "../src/pages/settings/SubjectsPage.vue";
import KnowledgePointsPage from "../src/pages/settings/KnowledgePointsPage.vue";
import SystemPage from "../src/pages/settings/SystemPage.vue";
import type { Component } from "vue";
import type { Document, Element as HappyElement } from "happy-dom";

const METRICS = {
  database: { size_bytes: 15000000, size_human: "14.3 MB", last_backup_at: "2026-09-30 03:00", schema_tag: "v1.0.0" },
  storage: { assets_bytes: 1200000000, assets_human: "1.1 GB", disk_total: 60000000000, disk_free: 58000000000, disk_used_ratio: 0.25, disk_supported: true },
  health: [],
  checked_at: "2026-10-02 10:00:00",
};

const BACKUPS = {
  items: [
    { timestamp: "2026-09-30 03:00", tier: "daily", file_name: "paperly-20260930.db", bytes: 15728640, integrity: "ok", created_at: "2026-09-30 03:00" },
  ],
  total: 1,
  last_backup_at: "2026-09-30 03:00",
  has_backup: true,
};

const SUBJECTS = [
  { id: 2, code: "math", name: "数学", sort_order: 0, source: 1 },
  { id: 5, code: "aoshu", name: "奥数思维", sort_order: 9, source: 2 },
];

/** Full dictionary the create/edit dialog loads (13-subject shape: mask + mandatory). */
const FORM_SUBJECTS = [
  { id: 1, code: "chinese", name: "语文", sort_order: 1, source: 1, mask: 31, mandatory: 1 },
  { id: 2, code: "math", name: "数学", sort_order: 2, source: 1, mask: 31, mandatory: 1 },
  { id: 3, code: "physics", name: "物理", sort_order: 3, source: 1, mask: 22, mandatory: 0 },
  { id: 4, code: "history", name: "历史", sort_order: 4, source: 1, mask: 14, mandatory: 0 },
];

const KIDS = [
  { id: 7, name: "小明", grade: 9, status: 1 },
  { id: 8, name: "小红", grade: 6, status: 2 },
  { id: 9, name: "小刚", grade: 3, status: 3 },
];

const RESPONSES: Record<string, unknown> = {
  // Longer paths must be listed before /api/subjects: the fetch stub matches by declaration order.
  "/api/subjects/2/knowledge_points": [],
  "/api/subjects/5/knowledge_points": [],
  "GET /api/subjects": SUBJECTS,
  "POST /api/subjects": { id: 9, name: "科学" },
  "PUT /api/subjects/9": { id: 9, name: "科学" },
  "/api/metrics/summary": METRICS,
  "/api/admin/backups": BACKUPS,
  "POST /api/admin/backup": { file_name: "paperly-now.db", bytes: 16000000 },
  "/api/admin/migrations": [
    { version: 1, name: "0001_init.sql", display_tag: "v1.0.0", applied_at: "2026-09-01 00:00:00" },
  ],
  "/api/admin/cursors": [
    { task_name: "backup_cron_job", last_id: 0, last_run_at: "2026-09-30 03:00:00", last_status: 2 },
  ],
  "GET /api/kids": KIDS,
  "POST /api/kids/9/restore": {
    id: 9, name: "小刚", grade: 3, gender: null, avatar: null,
    status: 1, created_at: "", updated_at: "",
  },
  // Longer paths must be listed before the bare /api/tasks: the fetch stub matches by
  // declaration order (substring), so /api/tasks/12 must precede /api/tasks.
  "GET /api/tasks/12": {
    id: 12, kind: 2, status: 2, priority: 0, retry_count: 0,
    created_at: "2026-10-12 10:00:00",
    last_error: "", started_at: "2026-10-12 10:00:01", finished_at: "2026-10-12 10:00:05",
    detail: '{"exam_id":4,"final_order":["9","12","11"],"delete_page_ids":[10]}',
  },
  "GET /api/tasks": {
    items: [
      {
        id: 12, kind: 2, status: 2, priority: 0, retry_count: 0,
        created_at: "2026-10-12 10:00:00",
        last_error: "", started_at: "2026-10-12 10:00:01", finished_at: "2026-10-12 10:00:05",
        // Real payload shape: compact one-line JSON (as stored by the worker).
        detail: '{"exam_id":4,"final_order":["9","12","11"],"delete_page_ids":[10]}',
      },
    ],
    next_cursor: 0, has_more: false, counts: {},
  },
  "/api/settings/image": { compress_preset: 3 },
};

async function mountSettingsPage(component: Component, path: string): Promise<MountResult> {
  const r = await mountPage(component, { path, responses: RESPONSES });
  await flush();
  return r;
}

const storage = () => mountSettingsPage(StoragePage, "/storage");
const image = () => mountSettingsPage(ImageSettingsPage, "/image");
const kids = () => mountSettingsPage(KidsAuthPage, "/kids-auth");
const tasks = () => mountSettingsPage(TasksPage, "/tasks");
const subjects = () => mountSettingsPage(SubjectsPage, "/subjects");
const knowledge = () => mountSettingsPage(KnowledgePointsPage, "/knowledge-points");
const system = () => mountSettingsPage(SystemPage, "/system");

function clickButton(doc: Document, label: string): void {
  const el = Array.from(doc.querySelectorAll("button")).find((b) =>
    (b.textContent ?? "").includes(label),
  );
  if (!el) throw new Error(`未找到按钮：${label}`);
  (el as unknown as { click: () => void }).click();
}

const suite: TestSuite = {
  name: "设置子页（7 个独立顶层页）",
  cases: [
    ["存储页渲染真实数据", async () => {
      const r = await storage();
      assertIncludes(r.text, "存储健康与冷备份", "存储区块");
      assertIncludes(r.text, "14.3 MB", "数据库大小来自 metrics");
    }],

    ["图片压缩页渲染真实数据", async () => {
      const r = await image();
      assertIncludes(r.text, "图片压缩", "图片压缩区块");
    }],

    ["孩子页渲染真实数据", async () => {
      const r = await kids();
      assertIncludes(r.text, "孩子与免密授权", "孩子区块");
      assertIncludes(r.text, "小明", "孩子列表");
    }],

    ["重置 PIN 弹窗：清除语义 + 邀请链接引导（P5b 修正旧「免 PIN」文案）", async () => {
      const r = await kids();
      const kidLi = Array.from(r.doc.querySelectorAll("li")).find((li) =>
        (li.textContent ?? "").includes("小明"),
      );
      assert(kidLi !== undefined, "应列出小明");
      const resetBtn = Array.from(kidLi!.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("重置 PIN"),
      );
      assert(resetBtn !== undefined, "active 孩子应有重置 PIN 按钮");
      (resetBtn as unknown as { click: () => void }).click();
      await flush();
      assertIncludes(
        r.text,
        "留空表示清除 PIN，孩子需重新通过邀请链接设置 PIN 后才能登录。",
        "P5b 新文案：清除后须重走邀请链接，不是「免 PIN 直接进入」",
      );
      assertNotIncludes(
        r.text,
        "免 PIN 直接进入",
        "旧文案暗示免 PIN 进入，与 P3/P5b 的邀请流相悖，不得回潮",
      );
    }],

    ["异步队列页渲染真实数据", async () => {
      const r = await tasks();
      assertIncludes(r.text, "异步与后台队列", "任务区块");
    }],

    ["异步队列：载荷默认收起，展开后 JSON 格式化 + 弹窗打开时锁定背景滚动", async () => {
      const r = await tasks();
      clickButton(r.doc, "日志");
      await flush();

      // Collapsed by default: the pretty payload is not rendered until expanded.
      assertNotIncludes(r.text, '"exam_id": 4,', "载荷默认应收起");

      // Expand the payload section, then the compact one-line payload shows as pretty JSON.
      clickButton(r.doc, "载荷");
      await flush();
      assertIncludes(r.text, '  "exam_id": 4,', "载荷应格式化为多行 JSON");

      // Scroll lock: <html> gets overflow:hidden while the dialog is open...
      assertEqual(
        (r.doc.documentElement as unknown as { style: { overflow: string } }).style.overflow,
        "hidden",
        "弹窗打开时 html 应 overflow:hidden",
      );

      // ...and released on close (Esc path).
      r.doc.defaultView!.dispatchEvent(
        new r.doc.defaultView!.KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
      );
      await flush();
      assertEqual(
        (r.doc.documentElement as unknown as { style: { overflow: string } }).style.overflow,
        "",
        "弹窗关闭后滚动锁应释放",
      );
    }],

    ["异步队列：失败任务展示「错误」块，展开后可见错误文本", async () => {
      const r = await mountPage(TasksPage, {
        path: "/tasks",
        responses: {
          ...RESPONSES,
          "GET /api/tasks/12": {
            id: 12, kind: 2, status: 3, priority: 0, retry_count: 0,
            created_at: "2026-10-12 10:00:00",
            last_error: "boom: page reorder failed",
            started_at: "2026-10-12 10:00:01", finished_at: "2026-10-12 10:00:05",
            detail: "{}",
          },
        },
      });
      await flush();
      clickButton(r.doc, "日志");
      await flush();

      assertIncludes(r.text, "错误", "失败任务应出现错误块");
      assertNotIncludes(r.text, "boom: page reorder failed", "错误默认收起");
      clickButton(r.doc, "错误");
      await flush();
      assertIncludes(r.text, "boom: page reorder failed", "展开后应显示错误文本");
    }],

    ["异步队列：无任务时「清理完成」禁用", async () => {
      const r = await mountPage(TasksPage, {
        path: "/tasks",
        responses: {
          ...RESPONSES,
          "GET /api/tasks": { items: [], next_cursor: 0, has_more: false, counts: {} },
        },
      });
      await flush();
      const clearBtn = Array.from(r.doc.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("清理完成"),
      );
      assert(clearBtn !== undefined, "应有清理完成按钮");
      assertEqual(
        (clearBtn as unknown as { disabled: boolean }).disabled,
        true,
        "无任务时「清理完成」应禁用",
      );
    }],

    ["异步队列：弹窗「关闭」按钮可关闭（替代已移除的右上角 X）", async () => {
      const r = await tasks();
      clickButton(r.doc, "日志");
      await flush();
      assertIncludes(r.text, "任务类型：", "弹窗已打开");
      clickButton(r.doc, "关闭");
      await flush();
      assertNotIncludes(r.text, "任务类型：", "点关闭后弹窗应消失");
    }],

    ["异步队列：复制载荷写入剪贴板", async () => {
      let copied = "";
      Object.defineProperty(globalThis.navigator, "clipboard", {
        configurable: true,
        value: { writeText: async (t: string) => { copied = t; } },
      });
      const r = await tasks();
      clickButton(r.doc, "日志");
      await flush();
      clickButton(r.doc, "复制");
      await flush();
      assert(copied.includes('"exam_id": 4'), "复制内容应为美化后的载荷 JSON");
      assertIncludes(r.text, "已复制到剪贴板", "复制后应给出提示");
    }],

    ["异步队列：清理完成后「清理完成」重新禁用", async () => {
      const responses: Record<string, unknown> = {
        ...RESPONSES,
        "POST /api/tasks/clear-finished": { deleted: 1 },
      };
      const r = await mountPage(TasksPage, { path: "/tasks", responses });
      await flush();

      const findClear = () =>
        Array.from(r.doc.querySelectorAll("button")).find((b) =>
          (b.textContent ?? "").includes("清理完成"),
        );
      assertEqual(
        (findClear() as unknown as { disabled: boolean }).disabled,
        false,
        "有任务时「清理完成」应可用",
      );

      (findClear() as unknown as { click: () => void }).click();
      await flush();
      const confirm = Array.from(r.doc.querySelectorAll("button")).find(
        (b) => (b.textContent ?? "").trim() === "清理",
      );
      assert(confirm !== undefined, "应有确认清理按钮");

      // The reload after clearing returns an empty list.
      responses["GET /api/tasks"] = { items: [], next_cursor: 0, has_more: false, counts: {} };
      (confirm as unknown as { click: () => void }).click();
      await flush();

      assertEqual(
        (findClear() as unknown as { disabled: boolean }).disabled,
        true,
        "清理后「清理完成」应重新禁用",
      );
    }],

    ["异步队列：状态筛选覆盖图例全部 6 种状态", async () => {
      const r = await tasks();
      const trigger = r.doc.querySelector('[role="combobox"]');
      if (!trigger) throw new Error("应有状态筛选 Select");
      (trigger as unknown as { click: () => void }).click();
      const options = Array.from(r.doc.querySelectorAll('[role="option"]')).map((o) =>
        (o.textContent ?? "").trim(),
      );
      for (const label of ["待执行", "执行中", "已完成", "失败", "已跳过", "已取消"]) {
        assert(options.includes(label), `状态筛选应含「${label}」，实际：${options.join("/")}`);
      }
    }],

    ["科目页渲染真实数据", async () => {
      const r = await subjects();
      assertIncludes(r.text, "科目管理", "科目区块");
      assertIncludes(r.text, "数学", "科目列表");
    }],

    ["科目列表不显示 code（内部标识不对家长暴露）", async () => {
      const r = await subjects();
      const section = r.doc.querySelector("#section-subjects");
      assert(section !== null, "应有科目区块");
      // The two subjects' codes are math / aoshu; neither should appear on screen.
      const text = section?.textContent ?? "";
      assertNotIncludes(text, "math", "科目行不应显示 code");
      assertNotIncludes(text, "aoshu", "科目行不应显示 code");
      // code could only appear as text inside <span class="...font-mono...">; pin
      // that form absent.
      assertEqual(
        section?.querySelectorAll("span.font-mono").length ?? 0,
        0,
        "科目区块不应有承载 code 的等宽文本节点",
      );
    }],

    ["新增 / 改名弹层无 code 输入控件", async () => {
      const r = await subjects();
      // Each dialog is mounted on show; locate the right one by its inner title.
      // Use happy-dom's Element (as in test/mount.ts; lib.dom's same-named type
      // is incompatible).
      const dialogByTitle = (title: string): HappyElement => {
        const hit = Array.from(r.doc.querySelectorAll('[role="dialog"]')).find((d) =>
          (d.textContent ?? "").includes(title),
        );
        if (!hit) throw new Error(`未找到「${title}」弹层`);
        return hit;
      };

      clickButton(r.doc, "添加科目");
      await flush();
      const addDlg = dialogByTitle("科目名称");
      assertEqual(
        addDlg.querySelectorAll('input[type="text"]').length,
        1,
        "新增弹层只应有「科目名称」一个输入框（无 code 输入）",
      );

      clickButton(r.doc, "改名");
      await flush();
      const renameDlg = dialogByTitle("新名称");
      assertEqual(
        renameDlg.querySelectorAll('input[type="text"]').length,
        1,
        "改名弹层只应有「新名称」一个输入框（无 code 输入）",
      );

      // The submit body carries only name: code is derived server-side, never
      // taken from user input.
      const input = renameDlg.querySelector('input[type="text"]');
      if (!input) throw new Error("改名弹层应有输入框");
      setNativeInput(input, "奥数");
      await flush();
      const saveBtn = Array.from(renameDlg.querySelectorAll("button")).find(
        (b) => (b.textContent ?? "").trim() === "保存",
      );
      if (!saveBtn) throw new Error("改名弹层应有保存按钮");
      (saveBtn as unknown as { click: () => void }).click();
      await flush();
      const call = r.calls.find((c) => c.method === "PUT" && c.url.includes("/api/subjects"));
      assert(call !== undefined, "应 PUT /api/subjects");
      const body = JSON.parse(call.body ?? "{}") as Record<string, unknown>;
      // assertEqual uses !==, so arrays must be joined before comparing.
      assertEqual(Object.keys(body).join(","), "name", "改名提交体只能有 name");
    }],

    ["知识点页渲染真实数据", async () => {
      const r = await knowledge();
      assertIncludes(r.text, "知识点管理", "知识点区块");
    }],

    ["系统页渲染迁移与游标", async () => {
      const r = await system();
      assertIncludes(r.text, "系统版本迁移与任务游标", "迁移区块");
      assertIncludes(r.text, "0001_init.sql", "迁移记录");
      assertIncludes(r.text, "backup_cron_job", "任务游标");
    }],

    ["系统内置科目不可删，自建科目可删", async () => {
      const r = await subjects();
      // The "system-provided" hint exists; only the self-created subject should
      // have a delete button.
      assertIncludes(r.text, "系统内置", "预置提示");
      const deleteButtons = Array.from(r.doc.querySelectorAll("button")).filter((b) =>
        (b.textContent ?? "").includes("删除"),
      );
      const subjectDelete = deleteButtons.filter((b) => {
        const li = b.closest("li");
        return (li?.textContent ?? "").includes("奥数思维") || (li?.textContent ?? "").includes("数学");
      });
      assertEqual(subjectDelete.length, 1, "仅自建科目有删除按钮");
    }],

    ["新增科目走弹层并 POST name", async () => {
      const r = await subjects();
      clickButton(r.doc, "添加科目");
      await flush();
      assertIncludes(r.text, "添加科目", "弹层标题");

      const input = r.doc.querySelector('input[type="text"]');
      if (!input) throw new Error("弹层内应有文本输入框");
      setNativeInput(input, "科学");
      await flush();

      // The "Add" button in the modal footer (exact text match, to avoid hitting the trigger).
      const addBtn = Array.from(r.doc.querySelectorAll("button")).find(
        (b) => (b.textContent ?? "").trim() === "添加",
      );
      assert(addBtn !== undefined, "弹层应有添加按钮");
      (addBtn as unknown as { click: () => void }).click();
      await flush();

      const call = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/subjects"));
      assert(call !== undefined, "应 POST /api/subjects");
      assertEqual(JSON.parse(call.body ?? "{}").name, "科学", "提交科目名");
    }],

    ["存储页「立即备份」触发 POST /api/admin/backup", async () => {
      const r = await storage();
      clickButton(r.doc, "立即备份");
      await flush();
      assert(
        r.calls.some((c) => c.method === "POST" && c.url.includes("/api/admin/backup")),
        "应 POST /api/admin/backup",
      );
    }],

    ["孩子页显示已删除项，且仅它可恢复", async () => {
      const r = await kids();
      assertIncludes(r.text, "已删除", "应显示已删除状态");
      const deletedLi = Array.from(r.doc.querySelectorAll("li")).find((li) =>
        (li.textContent ?? "").includes("小刚"),
      );
      assert(deletedLi !== undefined, "应列出已删除孩子");
      const labels = Array.from(deletedLi!.querySelectorAll("button")).map((b) =>
        b.textContent ?? "",
      );
      assert(labels.some((t) => t.includes("恢复")), "已删除项应有恢复按钮");
      assert(!labels.some((t) => t.includes("删除")), "已删除项不应再有删除按钮");
    }],

    ["恢复已删除孩子 POST /kids/:id/restore", async () => {
      const r = await kids();
      const kidsSection = r.doc.querySelector("#section-kids");
      assert(kidsSection !== null, "应有孩子区块");
      const restoreBtn = Array.from(kidsSection!.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("恢复"),
      );
      assert(restoreBtn !== undefined, "应有恢复按钮");
      (restoreBtn as unknown as { click: () => void }).click();
      await flush();
      assert(
        r.calls.some((c) => c.method === "POST" && c.url.includes("/api/kids/9/restore")),
        "应 POST /api/kids/9/restore",
      );
    }],

    ["已毕业孩子不提供删除与恢复", async () => {
      const r = await kids();
      const gradLi = Array.from(r.doc.querySelectorAll("li")).find((li) =>
        (li.textContent ?? "").includes("小红"),
      );
      assert(gradLi !== undefined, "应列出已毕业孩子");
      const labels = Array.from(gradLi!.querySelectorAll("button")).map((b) => b.textContent ?? "");
      assert(!labels.some((t) => t.includes("删除")), "已毕业不应有删除");
      assert(!labels.some((t) => t.includes("恢复")), "已毕业不应有恢复");
    }],

    ["编辑弹窗：必修课锁定 + 高中切文理科自动重算学科", async () => {
      // A senior-high (grade 10) science-track kid; the dictionary carries mask + mandatory.
      // Mount with dedicated responses: the form dialog needs the full 4-subject dictionary,
      // while the section list itself stays the shared 3-kid shape.
      const r = await mountPage(KidsAuthPage, {
        path: "/kids-auth",
        responses: {
          // Declaration order matters (substring matching): the whitelist route must come
          // before the detail route, or /api/kids/10/subjects matches GET /api/kids/10.
          "GET /api/kids/10/subjects": [
            { id: 1, code: "chinese", name: "语文", sort_order: 1, source: 1, mask: 31, mandatory: 1 },
            { id: 2, code: "math", name: "数学", sort_order: 2, source: 1, mask: 31, mandatory: 1 },
            { id: 3, code: "physics", name: "物理", sort_order: 3, source: 1, mask: 22, mandatory: 0 },
          ],
          "GET /api/kids/10": {
            id: 10, name: "小高", grade: 10, gender: 1, avatar: null,
            status: 1, track: 2, created_at: "", updated_at: "",
          },
          "GET /api/subjects": FORM_SUBJECTS,
          "GET /api/kids": [
            { id: 10, name: "小高", grade: 10, status: 1 },
          ],
        },
      });
      await flush();

      // Open the edit dialog for the senior-high kid.
      const editBtn = Array.from(r.doc.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("编辑"),
      );
      if (!editBtn) throw new Error("应有编辑按钮");
      (editBtn as unknown as { click: () => void }).click();
      await flush();

      // Mandatory subjects are locked (disabled checkbox + red * mark), others are free.
      const dialog = r.doc.querySelector('[role="dialog"]');
      if (!dialog) throw new Error("编辑弹窗应渲染");
      // Subjects are collapsed by default; expand the block before inspecting the grid.
      const subjectsToggle = Array.from(dialog.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("实际在读学科"),
      );
      if (!subjectsToggle) throw new Error("应有学科展开按钮");
      (subjectsToggle as unknown as { click: () => void }).click();
      await flush();
      const boxes = Array.from(dialog.querySelectorAll('input[type="checkbox"]'));
      assertEqual(boxes.length, 4, "应渲染 4 个学科复选框");
      const boxByLabel = (label: string): HappyElement => {
        const hit = boxes.find((b) =>
          (b.closest("label")?.textContent ?? "").includes(label),
        );
        if (!hit) throw new Error(`未找到「${label}」复选框`);
        return hit as unknown as HappyElement;
      };
      assertEqual(
        (boxByLabel("语文") as unknown as { disabled: boolean }).disabled,
        true,
        "必修课（语文）复选框应禁用",
      );
      assertIncludes((boxByLabel("语文").closest("label")?.textContent ?? ""), "*", "必修课红色 * 标记");
      assertNotIncludes((boxByLabel("物理").closest("label")?.textContent ?? ""), "*", "非必修无 * 标记");
      assertEqual(
        (boxByLabel("物理") as unknown as { disabled: boolean }).disabled,
        false,
        "非必修（物理）复选框可自由勾选",
      );
      assertEqual(
        (boxByLabel("物理") as unknown as { checked: boolean }).checked,
        true,
        "编辑初始回显已选的物理（理科 track=2）",
      );

      // Switch track arts: the auto-adjust must still run in the edit dialog (not create-only) —
      // physics (science-only mask 22) drops out, history (arts mask 14) comes in.
      const artsBtn = Array.from(dialog.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("文科"),
      );
      if (!artsBtn) throw new Error("高中编辑弹窗应有文科按钮");
      (artsBtn as unknown as { click: () => void }).click();
      await flush();
      assertEqual(
        (boxByLabel("物理") as unknown as { checked: boolean }).checked,
        false,
        "切文科后理科专属的物理应被自动取消",
      );
      assertEqual(
        (boxByLabel("历史") as unknown as { checked: boolean }).checked,
        true,
        "切文科后文科可见的历史应被自动勾选",
      );
      // Mandatory stays locked and checked through the recompute.
      assertEqual(
        (boxByLabel("语文") as unknown as { checked: boolean }).checked,
        true,
        "重算后必修课保持勾选",
      );
    }],

    ["图片压缩页渲染五档并回显当前档位", async () => {
      const r = await image();
      assertIncludes(r.text, "图片压缩", "图片压缩区块");
      assertIncludes(r.text, "无损保真", "第 1 档文案");
      assertIncludes(r.text, "有损极限压缩", "第 5 档文案");
      const radios = r.doc.querySelectorAll('input[name="webp-preset"]');
      assertEqual(radios.length, 5, "应有 5 个档位单选");
      const checked = Array.from(radios).filter((x) => (x as unknown as { checked: boolean }).checked);
      assertEqual(checked.length, 1, "恰有一个选中");
      assertEqual((checked[0] as unknown as { value: string }).value, "3", "回显接口档位 3");
    }],

    ["切换压缩档位 PUT /api/settings/image", async () => {
      const r = await image();
      const radio5 = r.doc.querySelector('input[name="webp-preset"][value="5"]');
      assert(radio5 !== null, "应有第 5 档");
      const el = radio5 as unknown as {
        checked: boolean;
        dispatchEvent: (e: unknown) => boolean;
      };
      el.checked = true;
      el.dispatchEvent(new r.doc.defaultView!.Event("change", { bubbles: true }));
      await flush();
      const call = r.calls.find((c) => c.method === "PUT" && c.url.includes("/api/settings/image"));
      assert(call !== undefined, "应 PUT /api/settings/image");
      assertEqual(JSON.parse(call.body ?? "{}").compress_preset, 5, "提交档位");
    }],

    ["科目行布局：无学段文字；自建科目删除按钮通栏危险色", async () => {
      const r = await subjects();
      const section = r.doc.querySelector("#section-subjects");
      assert(section !== null, "应有科目区块");
      const list = section!.querySelector("ul");
      if (!list) throw new Error("应有科目列表");

      // Stage is edited via the 学段 button, so no stage text is printed in the row.
      assertNotIncludes(list.textContent ?? "", "可见学段", "行内不应显示学段文字");
      const stageBtn = Array.from(list.querySelectorAll("button")).find(
        (b) => (b.textContent ?? "").includes("学段"),
      );
      const renameBtn = Array.from(list.querySelectorAll("button")).find(
        (b) => (b.textContent ?? "").includes("改名"),
      );
      assert(stageBtn !== undefined, "应有学段按钮");
      assert(renameBtn !== undefined, "应有改名按钮");
      assert(
        (stageBtn!.className ?? "").includes("w-full"),
        "学段按钮移动端应撑满等宽"
      );

      const delBtn = Array.from(list.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("删除"),
      );
      assert(delBtn !== undefined, "自建科目应有删除按钮");
      const cls = delBtn!.className ?? "";
      assert(cls.includes("w-full"), `删除按钮应通栏，实际 "${cls}"`);
      assert(cls.includes("bg-destructive"), `删除按钮应为 destructive 变体，实际 "${cls}"`);

      // System preset row keeps the actions but has no delete button.
      const systemLi = Array.from(list.querySelectorAll("li")).find((li) =>
        (li.textContent ?? "").includes("数学"),
      );
      assert(systemLi !== undefined, "应有系统内置科目行");
      assert(!(systemLi!.textContent ?? "").includes("删除"), "系统内置科目不可删");
    }],
  ],
};

export default suite;
