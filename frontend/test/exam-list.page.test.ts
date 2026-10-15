/**
 * test/exam-list.page.test.ts -- exam list page (maps to the exams DB table).
 *
 * Covers: server-side pagination/filtering, per-kid filtering, empty state, error
 * state, upload guidance. All filters are dropdowns (subject_id/grade/status are
 * structured values), so cases manipulate the select.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { setSelectAt as pickSelect, selectTriggers } from "./controls";
// happy-dom's and lib.dom's Element types differ; annotations use happy-dom's
// uniformly (see test/mount.ts).
import type { Element as HappyElement } from "happy-dom";
import { testWindow } from "./setup";
import { KID_ID_KEY, KID_NAME_KEY, KID_GRADE_KEY } from "../src/lib/kidSession";
import ExamListPage from "../src/pages/ExamListPage.vue";
import type { Document } from "happy-dom";

/** Response data matching backend GET /api/exams (title/subject_id/grade:int). */
const FIXTURE = {
  items: [
    {
      id: 5,
      title: "2024-初三-上学期-数学-11月-期中",
      subject_id: 2,
      grade: 9,
      exam_type: 3,
      exam_date: "2024-11-01",
      kid_id: 7,
      uploaded_by: 1,
      status: 1,
      page_count: 3,
    },
    {
      id: 4,
      title: "英语模拟卷 A",
      subject_id: 3,
      exam_type: 5,
      exam_date: "2024-10-12",
      uploaded_by: 2,
      status: 1,
      page_count: 2,
    },
  ],
  next_cursor: 0,
  has_more: false,
};

const SUBJECTS = [
  { id: 2, code: "math", name: "数学", sort_order: 0 },
  { id: 3, code: "english", name: "英语", sort_order: 0 },
];

const KIDS = [
  { id: 7, name: "小明", grade: 9, status: 1 },
  { id: 8, name: "小红", grade: 8, status: 1 },
];

async function mountExams(list: unknown = FIXTURE, path = "/exams"): Promise<MountResult> {
  return mountPage(ExamListPage, {
    path,
    responses: { "/api/exams": list, "/api/subjects": SUBJECTS, "/api/kids": KIDS },
  });
}

/** All requested /api/exams URLs (for asserting ownership filter params). */
function examRequests(r: MountResult): string[] {
  return r.requests.filter((u) => u.includes("/api/exams"));
}

function findButton(doc: Document, label: string): unknown {
  const el = Array.from(doc.querySelectorAll("button")).find((b) =>
    (b.textContent ?? "").includes(label),
  );
  if (!el) throw new Error(`未找到按钮：${label}`);
  return el;
}

function clickButton(doc: Document, label: string): void {
  (findButton(doc, label) as unknown as { click: () => void }).click();
}

function isDisabled(button: unknown): boolean {
  return (button as { disabled?: boolean }).disabled === true;
}

/** On the parent side, Select 0 is the kid filter, 1 is subject, 2 is grade, 3 is status. */
function setSelectAt(doc: Document, index: number, value: string): void {
  pickSelect(doc, index, value);
}

const suite: TestSuite = {
  name: "ExamListPage（列表 / 分页 / 筛选）",
  cases: [
    ["渲染服务端返回的行与关键字段", async () => {
      const r = await mountExams();
      await flush();
      assertEqual(r.doc.querySelectorAll("tbody tr").length, 2, "行数");
      assertIncludes(r.text, "2024-初三-上学期-数学-11月-期中", "试卷标题");
      assertIncludes(r.text, "数学", "科目名（由 subject_id 映射）");
      assertIncludes(r.text, "初三", "年级（由 INTEGER 9 映射）");
      assertIncludes(r.text, "小明", "归属应显示孩子名而非 #id");
      assertIncludes(r.text, "3 页", "页数");
    }],

    ["归属孩子查不到时退回「学生 #id」而非空", async () => {
      const ORPHAN = { items: [{ ...FIXTURE.items[0], kid_id: 999 }], next_cursor: 0, has_more: false };
      const r = await mountExams(ORPHAN, "/exams?kid_id=999");
      await flush();
      assertIncludes(r.text, "学生 #999", "查不到孩子时应退回 #id");
    }],

    ["家长端首列是「归属」，逐行显示孩子名", async () => {
      const r = await mountExams();
      await flush();
      const headers = Array.from(r.doc.querySelectorAll("thead th")).map(
        (th) => (th.textContent ?? "").trim(),
      );
      assertEqual(headers[0], "归属", `首列应是归属，实际：${headers.join(",")}`);
      const firstRow = r.doc.querySelector("tbody tr");
      assert(firstRow !== null, "应有数据行");
      assertIncludes(firstRow!.textContent ?? "", "小明", "第一行归属应显示小明");
    }],

    ["空列表显示「暂无试卷」而非空表", async () => {
      const r = await mountExams({ items: [], next_cursor: 0, has_more: false });
      assertIncludes(r.text, "暂无试卷", "空态文案");
      assertEqual(r.doc.querySelectorAll("tbody tr").length, 0, "空态不渲染数据行");
    }],

    ["家长端孩子筛选器排第一，默认「孩子: 全部」", async () => {
      const r = await mountExams();
      await flush();
      const trigger = selectTriggers(r.doc)[0];
      assert(trigger !== undefined, "应有孩子筛选 Select");
      assertIncludes(trigger?.textContent ?? "", "孩子: 全部", "默认全部");
      const panel = trigger?.closest("[data-select-root]");
      assert(panel !== null, "Select 根节点应带 data-select-root");
      const labels = Array.from(panel!.querySelectorAll('[role="option"]')).map(
        (o) => (o.querySelector("span.truncate")?.textContent ?? "").trim(),
      );
      assertEqual(labels.length, 3, `选项应为「全部 + 2 个孩子」，实际：${labels.join(" / ")}`);
      assertEqual(labels[0], "孩子: 全部", "第 1 项是全部");
      assertEqual(labels[1], "小明", "第 2 项是小明");
      assertEqual(labels[2], "小红", "第 3 项是小红");
    }],

    ["首次加载请求带默认分页，且不带空筛选项", async () => {
      const r = await mountExams();
      const list = r.requests.find((u) => u.includes("/api/exams")) ?? "";
      assert(list.includes("limit=10"), `带页大小，实际 ${list}`);
      assertNotIncludes(list, "cursor=", "第一页不应带游标");
      assertNotIncludes(list, "subject_id=", "空科目不应下发");
      assertNotIncludes(list, "grade=", "空年级不应下发");
      assertNotIncludes(list, "status=", "空状态不应下发");
      // The kid filter defaults to "all" rather than the top bar's current kid,
      // so no kid_id is sent.
      assertNotIncludes(list, "kid_id=", `默认「全部」不应下发 kid_id，实际 ${list}`);
    }],

    ["按科目筛选后重新请求并回到第 1 页", async () => {
      const r = await mountExams();
      await flush();
      const before = r.requests.length;
      setSelectAt(r.doc, 1, "2");
      await flush();
      assertEqual(r.requests.length, before + 1, "筛选应重新取数");
      const last = r.requests[r.requests.length - 1] ?? "";
      assert(last.includes("subject_id=2"), `带科目 id，实际 ${last}`);
      assertNotIncludes(last, "cursor=", `筛选后回到第 1 页（无游标），实际 ${last}`);
    }],

    ["状态筛选器有且只有 3 个选项，默认「全部状态」", async () => {
      const r = await mountExams();
      await flush();
      const trigger = selectTriggers(r.doc)[3];
      assert(trigger !== undefined, "应有第 4 个 Select（状态筛选）");
      assertIncludes(trigger?.textContent ?? "", "全部状态", "默认选中「全部状态」");

      // Select panels are always mounted (hidden toggles visibility); scope tightly
      // to the status filter's panel via data-select-root, since every Select's
      // options coexist in the DOM.
      const panel = trigger?.closest("[data-select-root]");
      assert(panel !== null, "Select 根节点应带 data-select-root");
      // Options contain a check Icon (PUA codepoint), so read only the label
      // span's text.
      const labels = Array.from(panel!.querySelectorAll('[role="option"]')).map(
        (o) => (o.querySelector("span.truncate")?.textContent ?? "").trim(),
      );
      assertEqual(labels.length, 3, `状态筛选应恰好 3 个选项，实际：${labels.join(" / ")}`);
      assertEqual(labels[0], "全部状态", "第 1 项是全部");
      assertEqual(labels[1], "正常", "第 2 项是正常");
      assertEqual(labels[2], "已删除", "第 3 项是已删除");
    }],

    ["默认「全部」不下发 status 限制（后端即含已删除）", async () => {
      const r = await mountExams();
      await flush();
      const first = examRequests(r)[0] ?? "";
      assertNotIncludes(first, "status=", `「全部」不应带 status 限制，实际 ${first}`);
    }],

    ["选「正常」下发 status=1", async () => {
      const r = await mountExams();
      await flush();
      setSelectAt(r.doc, 3, "1");
      await flush();
      const last = r.requests[r.requests.length - 1] ?? "";
      assert(last.includes("status=1"), `「正常」应下发 status=1，实际 ${last}`);
    }],

    ["按状态筛选下发整数枚举（已删除 = status=2）", async () => {
      const r = await mountExams();
      await flush();
      setSelectAt(r.doc, 3, "2");
      await flush();
      const last = r.requests[r.requests.length - 1] ?? "";
      assert(last.includes("status=2"), `状态为整数枚举，实际 ${last}`);
    }],

    ["切回「全部」后请求恢复为不含状态限制", async () => {
      const r = await mountExams();
      await flush();
      setSelectAt(r.doc, 3, "2");
      await flush();
      assertIncludes(
        r.requests[r.requests.length - 1] ?? "",
        "status=2",
        "先切到已删除",
      );

      setSelectAt(r.doc, 3, "0");
      await flush();
      const last = r.requests[r.requests.length - 1] ?? "";
      assertNotIncludes(last, "status=", `切回「全部」应去掉 status，实际 ${last}`);
      assertNotIncludes(last, "kid_id=", `切回「全部」应保持孩子全部，实际 ${last}`);
    }],

    ["URL 带 kid_id 时孩子筛选器预选该孩子", async () => {
      const r = await mountExams(FIXTURE, "/exams?kid_id=8");
      await flush();
      const trigger = selectTriggers(r.doc)[0];
      assertIncludes(trigger?.textContent ?? "", "小红", "孩子筛选器应预选小红");
      const list = examRequests(r)[0] ?? "";
      assert(list.includes("kid_id=8"), `深链应预选孩子过滤，实际 ${list}`);
    }],

    ["孩子筛选切回「全部」后请求去掉 kid_id", async () => {
      const r = await mountExams(FIXTURE, "/exams?kid_id=8");
      await flush();
      setSelectAt(r.doc, 0, "0");
      await flush();
      const last = examRequests(r).at(-1) ?? "";
      assertNotIncludes(last, "kid_id=", `切回全部应去掉 kid_id，实际 ${last}`);
    }],

    ["默认请求不带 kid_id（默认域 = 全部孩子）", async () => {
      const r = await mountExams(FIXTURE, "/exams");
      await flush();
      const list = examRequests(r)[0] ?? "";
      assertNotIncludes(list, "kid_id=", `默认应为全部，实际 ${list}`);
    }],

    ["用筛选器切孩子后重新取数（回第 1 页）", async () => {
      const PAGE1 = { items: FIXTURE.items, next_cursor: 5, has_more: true };
      const r = await mountExams(PAGE1, "/exams");
      await flush();
      // Go to page 2 so the upcoming filter change starts from a paged state
      clickButton(r.doc, "下一页");
      await flush();
      assertEqual(examRequests(r).length, 2, "翻页应发第二次请求");

      setSelectAt(r.doc, 0, "8");
      await flush();
      const last = examRequests(r).at(-1) ?? "";
      assert(last.includes("kid_id=8"), `筛选孩子应重新取数，实际 ${last}`);
      assert(!last.includes("cursor="), `筛选应回到第 1 页，实际 ${last}`);
    }],

    ["没有孩子时不带归属过滤，退化为显示全部", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/exams",
        responses: { "/api/exams": FIXTURE, "/api/subjects": SUBJECTS, "/api/kids": [] },
      });
      await flush();
      const list = examRequests(r)[0] ?? "";
      assert(!list.includes("kid_id"), `无孩子时不应下发 kid_id，实际 ${list}`);
    }],

    ["游标翻页：第一页无游标、下一页带 next_cursor、上一页弹栈", async () => {
      const PAGE = { items: FIXTURE.items, next_cursor: 5, has_more: true };
      const r = await mountPage(ExamListPage, {
        path: "/exams",
        responses: { "/api/exams": PAGE, "/api/subjects": SUBJECTS },
      });
      await flush();
      assert(isDisabled(findButton(r.doc, "上一页")), "第 1 页的上一页应禁用");

      const before = r.requests.length;
      clickButton(r.doc, "下一页");
      await flush();
      assertEqual(r.requests.length, before + 1, "翻页应重新取数");
      const last = r.requests[r.requests.length - 1] ?? "";
      assert(last.includes("cursor=5"), `第二页应带 next_cursor，实际 ${last}`);

      // Previous page should pop back to page 1 (no cursor).
      clickButton(r.doc, "上一页");
      await flush();
      const back = r.requests[r.requests.length - 1] ?? "";
      assertNotIncludes(back, "cursor=", `上一页应回到无游标，实际 ${back}`);
    }],

    ["请求失败时显示错误，不谎报为空", async () => {
      const r = await mountExams({ code: 50001, message: "读取试卷失败" });
      assertIncludes(r.text, "读取试卷失败", "错误信息上屏");
      assertNotIncludes(r.text, "暂无试卷", "有错误时不应同时显示空态");
      assertEqual(r.errors.length, 0, "错误应由页面处理，不冒泡成未捕获异常");
    }],

    ["错误态可点重试重新取数", async () => {
      const r = await mountExams({ code: 50001, message: "读取试卷失败" });
      const before = r.requests.length;
      clickButton(r.doc, "重试");
      await flush();
      assertEqual(r.requests.length, before + 1, "重试应重新请求");
    }],

    ["空态带上传引导动作", async () => {
      const r = await mountExams({ items: [], next_cursor: 0, has_more: false });
      clickButton(r.doc, "上传第一份试卷");
      await flush();
      assert(r.navigations.includes("/exams/upload"), `应跳转上传，实际 ${r.navigations.join(",")}`);
    }],

    ["无控制台错误", async () => {
      const r = await mountExams();
      await flush();
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],

    // Titles link to detail; there is no trailing "detail" button column (matching home).
    ["无「详情」按钮列，标题是指向详情的链接", async () => {
      const r = await mountExams();
      await flush();
      const detailBtn = Array.from(r.doc.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").trim() === "详情",
      );
      assertEqual(detailBtn, undefined, "不应再有详情按钮");
      const headers = Array.from(r.doc.querySelectorAll("thead th")).map((th) => th.textContent ?? "");
      assert(!headers.includes("操作"), `不应再有操作列，实际 ${headers.join(",")}`);
      const link = r.doc.querySelector('a[href="/exams/5"]');
      assert(link !== null, "试卷标题应是指向详情的链接");
      assertIncludes(link.textContent ?? "", "2024-初三-上学期-数学-11月-期中", "链接文案是标题");
    }],

    // --- Kid-side routes (/kid/exams, meta.audience="kid") ---
    ["孩子端不显示孩子筛选，只看自己的卷子", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/kid/exams",
        storage: { [KID_ID_KEY]: "7", [KID_NAME_KEY]: "小明" },
        responses: { "/api/exams": FIXTURE, "/api/subjects": SUBJECTS },
      });
      await flush();
      assertNotIncludes(r.text, "正在浏览", "归属提示对孩子毫无意义");
      assertNotIncludes(r.text, "孩子: 全部", "孩子端不应有孩子筛选");
      const firstTrigger = selectTriggers(r.doc)[0];
      assertIncludes(firstTrigger?.textContent ?? "", "科目: 全部", "孩子端第一个筛选是科目");
      const list = examRequests(r)[0] ?? "";
      assert(list.includes("kid_id=7"), `孩子端应按会话 kidId 过滤，实际 ${list}`);
    }],

    ["孩子端年级筛选默认孩子当前年级（会话 grade）", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/kid/exams",
        storage: { [KID_ID_KEY]: "7", [KID_NAME_KEY]: "小明", [KID_GRADE_KEY]: "9" },
        responses: { "/api/exams": FIXTURE, "/api/subjects": SUBJECTS },
      });
      await flush();
      // The kid side doesn't render the kid filter; Select order is
      // subject/grade/status. Grade (index 1) defaults to the session kid's
      // current grade (grade 9), and the request should carry grade=9.
      const gradeTrigger = selectTriggers(r.doc)[1];
      assertIncludes(gradeTrigger?.textContent ?? "", "初三", "年级筛选应默认为孩子当前年级");
      const list = examRequests(r)[0] ?? "";
      assert(list.includes("grade=9"), `孩子端应按会话年级过滤，实际 ${list}`);
    }],

    ["孩子端不请求家长端 /api/kids", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/kid/exams",
        storage: { [KID_ID_KEY]: "7" },
        responses: { "/api/exams": FIXTURE, "/api/subjects": SUBJECTS },
      });
      await flush();
      assert(!r.requests.some((u) => u.includes("/api/kids")), "孩子端不应拉孩子列表");
    }],

    ["孩子端列表没有「归属」列（每行都是自己）", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/kid/exams",
        storage: { [KID_ID_KEY]: "7" },
        responses: { "/api/exams": FIXTURE, "/api/subjects": SUBJECTS },
      });
      await flush();
      const headers = Array.from(r.doc.querySelectorAll("thead th")).map((th) => th.textContent ?? "");
      assert(!headers.includes("归属"), `孩子端不该有归属列，实际 ${headers.join(",")}`);
    }],

    ["孩子端列表标题链接指向 /kid/exams/:id，上传按钮跳 /kid/exams/upload", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/kid/exams",
        storage: { [KID_ID_KEY]: "7" },
        responses: { "/api/exams": FIXTURE, "/api/subjects": SUBJECTS },
      });
      await flush();
      const link = r.doc.querySelector('a[href="/kid/exams/5"]');
      assert(link !== null, "孩子端标题应指向 /kid/exams/:id");
      clickButton(r.doc, "上传试卷");
      await flush();
      assert(r.navigations.includes("/kid/exams/upload"), `应跳上传，实际 ${r.navigations.join(",")}`);
    }],

    ["孩子端空态引导跳孩子端上传页", async () => {
      const r = await mountPage(ExamListPage, {
        path: "/kid/exams",
        storage: { [KID_ID_KEY]: "7" },
        responses: { "/api/exams": { items: [], next_cursor: 0, has_more: false }, "/api/subjects": SUBJECTS },
      });
      await flush();
      clickButton(r.doc, "上传第一份试卷");
      await flush();
      assert(r.navigations.includes("/kid/exams/upload"), `应跳孩子端上传，实际 ${r.navigations.join(",")}`);
    }],
  ],
};

export default suite;
