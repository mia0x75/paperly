/**
 * test/button-variant.test.ts -- button visual consistency.
 *
 * The `default` variant is `bg-primary`, which in dark mode is near-white
 * (#e5e5e5) while the outline variant (`bg-control`) is dark (#1a1a1a). The
 * upload / add / back-up / register-kid / add-subject / add-knowledge-point
 * buttons are unified to the outline variant, so both are dark-backed and don't
 * clash on the same page.
 *
 * We assert "not bg-primary" rather than the full class string: Tailwind merge
 * makes class order unstable, and substring checks survive that while still
 * catching a revert to the default variant.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import type { Component } from "vue";
import type { Document } from "happy-dom";
import StoragePage from "../src/pages/settings/StoragePage.vue";
import KidsAuthPage from "../src/pages/settings/KidsAuthPage.vue";
import SubjectsPage from "../src/pages/settings/SubjectsPage.vue";
import KnowledgePointsPage from "../src/pages/settings/KnowledgePointsPage.vue";
import ExamListPage from "../src/pages/ExamListPage.vue";
import ExamCreatePage from "../src/pages/ExamCreatePage.vue";

const KIDS = [{ id: 7, name: "小明", grade: 9, status: 1 }];
const SUBJECTS = [{ id: 2, code: "math", name: "数学", sort_order: 0, source: 1 }];
const EXAM_LIST = { items: [], next_cursor: 0, has_more: false };
const METRICS = {
  database: { size_bytes: 1, size_human: "1 B", last_backup_at: null, schema_tag: "v1.0.0" },
  storage: {
    assets_bytes: 0, assets_human: "0 B", disk_total: 1, disk_free: 1,
    disk_used_ratio: 0.1, disk_supported: true,
  },
  health: [],
  checked_at: "2026-10-02 10:00:00",
};
const BACKUPS = { items: [], total: 0, last_backup_at: null, has_backup: false };

/** Find a button by visible text (Material Symbols are font ligatures, so use includes). */
function button(doc: Document, label: string): HTMLElement {
  const el = Array.from(doc.querySelectorAll("button")).find((b) =>
    (b.textContent ?? "").includes(label),
  );
  if (!el) throw new Error(`button not found: ${label}`);
  return el as unknown as HTMLElement;
}

function assertNotPrimary(doc: Document, label: string): void {
  const cls = button(doc, label).className;
  assert(
    !/\bbg-primary\b/.test(cls),
    `"${label}" must not use the default(bg-primary) variant, which is white-backed in dark mode; actual class=${cls}`,
  );
}

/**
 * These four buttons live on different settings sub-pages, so the mount target is
 * located by label. Each case re-mounts (mountPage resets the DOM).
 */
const SETTING_BUTTON_PAGES: Array<{ label: string; component: Component; path: string }> = [
  { label: "立即备份", component: StoragePage, path: "/storage" },
  { label: "新登记孩子", component: KidsAuthPage, path: "/kids-auth" },
  { label: "添加科目", component: SubjectsPage, path: "/subjects" },
  { label: "添加知识点", component: KnowledgePointsPage, path: "/knowledge-points" },
];

const RESPONSES: Record<string, unknown> = {
  "/api/admin/backup": BACKUPS,
  "/api/metrics": METRICS,
  "/api/subjects": SUBJECTS,
  "/api/kids": KIDS,
  "/api/knowledge/points": { items: [] },
  "/api/tasks": { items: [] },
};

async function mountSetting(label: string): Promise<MountResult> {
  const target = SETTING_BUTTON_PAGES.find((p) => p.label === label);
  if (!target) throw new Error(`sub-page owning button not registered: ${label}`);
  const r = await mountPage(target.component, { path: target.path, responses: RESPONSES });
  await flush();
  return r;
}

const suite: TestSuite = {
  name: "按钮变体一致性（统一到 outline 黑底）",
  cases: [
    ["设置页四个按钮都不是 default 变体", async () => {
      for (const { label } of SETTING_BUTTON_PAGES) {
        const r = await mountSetting(label);
        assertNotPrimary(r.doc, label);
      }
    }],

    ["试卷列表「上传试卷」不是 default 变体", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/exams",
        responses: { "/api/exams": EXAM_LIST, "/api/kids": KIDS, "/api/subjects": SUBJECTS },
      });
      await flush();
      assertNotPrimary(r.doc, "上传试卷");
    }],

    ["录入页「新增」不是 default 变体", async () => {
      const r = await mountPage(ExamCreatePage, {
        path: "/exams/upload",
        responses: { "/api/kids": KIDS, "/api/subjects": SUBJECTS },
      });
      await flush();
      assertNotPrimary(r.doc, "新增");
    }],

    ["这批按钮统一走 bg-control（暗色 #1a1a1a / 亮色 #f5f5f5）", async () => {
      for (const { label } of SETTING_BUTTON_PAGES) {
        const r = await mountSetting(label);
        const cls = button(r.doc, label).className;
        assert(
          /\bbg-control\b/.test(cls),
          `"${label}" should use bg-control (= the outline variant's backing), actual class=${cls}`,
        );
      }
    }],

    ["取消/返回等次要按钮仍是 outline，未被误改", async () => {
      // Reverse guard: only the targeted buttons were unified, not every button.
      const r = await mountPage(ExamCreatePage, {
        path: "/exams/upload",
        responses: { "/api/kids": KIDS, "/api/subjects": SUBJECTS },
      });
      await flush();
      const cancel = button(r.doc, "取消");
      assertEqual(
        /\bbg-control\b/.test(cancel.className),
        true,
        `"取消" stays outline (bg-control)`,
      );
    }],
  ],
};

export default suite;
