/**
 * test/kids.page.test.ts -- kid list page.
 *
 * Covers: server-side filtering (status/grade), reset, empty state, enter detail, error.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { setNativeInput, setSelectAt as pickSelect } from "./controls";
import { testWindow } from "./setup";
import KidsPage from "../src/pages/KidsPage.vue";
import type { Document, Element as HappyElement } from "happy-dom";

const STUDENTS = [
  { id: 7, name: "小明", grade: 9, status: 1 },
  { id: 8, name: "小红", grade: 8, status: 1 },
];

/** Subject dictionary the create/edit dialog loads to build the kid's study set. */
const SUBJECTS = [
  { id: 1, code: "chinese", name: "语文", sort_order: 1, source: 1, mask: 31, mandatory: 1 },
  { id: 2, code: "math", name: "数学", sort_order: 2, source: 1, mask: 31, mandatory: 1 },
  { id: 3, code: "english", name: "英语", sort_order: 3, source: 1, mask: 31, mandatory: 0 },
];

async function mountKids(list: unknown = STUDENTS): Promise<MountResult> {
  return mountPage(KidsPage, {
    path: "/kids",
    responses: { "GET /api/kids": list, "GET /api/subjects": SUBJECTS },
  });
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

/** There are two Selects on the page: grade (index 0) first, status (index 1) after. */
function setSelectAt(doc: Document, index: number, value: string): void {
  pickSelect(doc, index, value);
}

function setGrade(doc: Document, value: number): void {
  setSelectAt(doc, 0, String(value));
}

function setStatus(doc: Document, value: number): void {
  setSelectAt(doc, 1, String(value));
}

const lastUrl = (r: MountResult): string => r.requests[r.requests.length - 1] ?? "";

const suite: TestSuite = {
  name: "KidsPage（列表 / 筛选）",
  cases: [
    ["渲染学生行与关键字段", async () => {
      const r = await mountKids();
      assertEqual(r.doc.querySelectorAll("tbody tr").length, 2, "行数");
      assertIncludes(r.text, "小明", "姓名");
      assertIncludes(r.text, "初三", "年级");
      assertIncludes(r.text, "在读", "状态中文");
    }],

    ["空列表显示空态", async () => {
      const r = await mountKids([]);
      assertIncludes(r.text, "还没有学生", "空态文案");
      assertEqual(r.doc.querySelectorAll("tbody tr").length, 0, "无数据行");
    }],

    ["首次加载不带筛选参数", async () => {
      const r = await mountKids();
      assertEqual(r.requests.length, 1, "只请求一次");
      const url = lastUrl(r);
      assert(url.includes("/api/kids"), `命中端点，实际 ${url}`);
      assertNotIncludes(url, "status=", "不应带状态");
      assertNotIncludes(url, "grade=", "不应带年级");
    }],

    ["按状态筛选后重新请求", async () => {
      const r = await mountKids();
      setStatus(r.doc, 3);
      await flush();
      assertEqual(r.requests.length, 2, "筛选应重新取数");
      assertIncludes(lastUrl(r), "status=3", "带状态参数");
    }],

    ["按年级筛选后重新请求", async () => {
      const r = await mountKids();
      setGrade(r.doc, 9);
      await flush();
      assertIncludes(lastUrl(r), "grade=9", "带年级参数");
      assertEqual(r.errors.length, 0, "不崩");
    }],

    ["重置清空筛选并重新请求", async () => {
      const r = await mountKids();
      setStatus(r.doc, 3);
      await flush();
      setGrade(r.doc, 9);
      await flush();
      clickButton(r.doc, "重置");
      await flush();
      const url = lastUrl(r);
      assertNotIncludes(url, "status=", "重置后无状态");
      assertNotIncludes(url, "grade=", "重置后无年级");
    }],

    ["点详情进入学生详情页", async () => {
      const r = await mountKids();
      clickButton(r.doc, "详情");
      await flush();
      assert(r.navigations.includes("/kids/7"), `应进入详情，实际 ${r.navigations.join(",")}`);
    }],

    ["加载失败显示错误且不崩", async () => {
      const r = await mountKids({ code: 50000, message: "读取学生失败" });
      assertIncludes(r.text, "读取学生失败", "错误上屏");
      assertEqual(r.errors.length, 0, "不冒泡成未捕获异常");
    }],

    ["错误态可点重试重新取数", async () => {
      const r = await mountKids({ code: 50000, message: "读取学生失败" });
      clickButton(r.doc, "重试");
      await flush();
      assertEqual(r.requests.length, 2, "重试应重新请求");
    }],

    ["空态创建引导在当前页弹出登记弹层，不跳独立页", async () => {
      const r = await mountKids([]);
      clickButton(r.doc, "创建第一个学生");
      await flush();
      assertIncludes(r.text, "新登记孩子", "应弹出登记表单");
      assert(!r.navigations.includes("/kids/new"), "不应再跳 /kids/new");
    }],

    ["新增学生弹层：学科默认折叠，展开后显示网格", async () => {
      const r = await mountKids([]);
      clickButton(r.doc, "创建第一个学生");
      await flush();
      const dialog = r.doc.querySelector('[role="dialog"]');
      assert(dialog, "弹窗应渲染");
      const toggle = Array.from(dialog!.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("实际在读学科"),
      );
      assert(toggle, "应有学科展开按钮");
      assertEqual((toggle as unknown as { getAttribute: (k: string) => string | null }).getAttribute("aria-expanded"), "false", "默认折叠");
      // collapsed: checkboxes not rendered
      assertEqual(dialog!.querySelectorAll('input[type="checkbox"]').length, 0, "折叠态不渲染复选框");
      // expand
      (toggle as unknown as { click: () => void }).click();
      await flush();
      assertEqual(dialog!.querySelectorAll('input[type="checkbox"]').length, 3, "展开后渲染学科复选框");
      assertEqual((toggle as unknown as { getAttribute: (k: string) => string | null }).getAttribute("aria-expanded"), "true", "展开后 aria-expanded 为 true");
    }],

    ["新增学生弹层：展开学科后仅学科列表滚动，表单字段不随之滚动", async () => {
      const r = await mountKids([]);
      clickButton(r.doc, "创建第一个学生");
      await flush();
      const dialog = r.doc.querySelector('[role="dialog"]');
      assert(dialog, "弹窗应渲染");
      const toggle = Array.from(dialog!.querySelectorAll("button")).find((b) =>
        (b.textContent ?? "").includes("实际在读学科"),
      );
      assert(toggle, "应有学科展开按钮");
      (toggle as unknown as { click: () => void }).click();
      await flush();

      const nameInput = dialog!.querySelector('input[type="text"]');
      const pinInput = dialog!.querySelector('input[placeholder*="4-6"]');
      const box = dialog!.querySelector('input[type="checkbox"]');
      assert(nameInput, "应有姓名输入框");
      assert(pinInput, "应有 PIN 输入框");
      assert(box, "应有学科复选框");

      // The nearest overflow-y-auto ancestor of a checkbox is the scroll region.
      let scroller: HappyElement | null = box.parentElement;
      while (scroller && !scroller.className.includes("overflow-y-auto")) scroller = scroller.parentElement;
      assert(scroller, "学科列表应有独立滚动容器");
      assert(!scroller!.contains(nameInput), "姓名不应在学科滚动区内（滚动时不随动）");
      assert(!scroller!.contains(pinInput), "PIN 不应在学科滚动区内（滚动时不随动）");
      // The list reserves ~5 rows so the usual subject set fits without a scrollbar.
      assert(
        scroller!.className.includes("min-h-[8rem]"),
        "学科滚动区应预留约 5 行高度",
      );

      const body = dialog!.querySelector('[data-testid="modal-body"]');
      assert(body, "应有正文容器");
      assert(body!.className.includes("flex-col"), "弹窗正文应为 flex 列，字段作为不滚动项固定");
    }],

    ["新增学生弹层：提交创建并展示一次性访问链接", async () => {
      const r = await mountPage(KidsPage, {
        path: "/kids",
        responses: {
          "GET /api/kids": STUDENTS,
          "GET /api/subjects": SUBJECTS,
          "POST /api/kids": {
            id: 9,
            name: "小刚",
            access_code: "abcd2345",
            kid_url: "/kid/abcd2345",
            created_at: "2026-10-02T09:00:00Z",
          },
        },
      });
      await flush();
      clickButton(r.doc, "新增学生");
      await flush();
      assertIncludes(r.text, "新登记孩子", "应弹出登记表单");

      // The first text input in the modal is the name (the PIN field follows it).
      setNativeInput(r.doc.querySelector('.fixed input[type="text"]'), "小刚");
      // The page's two filter Selects come first, so the modal's grade is the 3rd
      // combobox (index 2).
      setSelectAt(r.doc, 2, "9");
      await flush();
      clickButton(r.doc, "创建");
      await flush();

      assert(
        r.calls.some((c) => c.method === "POST" && c.url.includes("/api/kids")),
        "应发起创建请求",
      );
      const createCall = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/kids"));
      assert(createCall, "应发起创建请求");
      try {
        const body = JSON.parse(createCall?.body ?? "{}");
        assert(Array.isArray(body.subject_ids), "折叠态提交时应包含 subject_ids 数组（默认学科）");
      } catch (e) {
        assert(false, `POST body 应为 JSON，${String(e)}`);
      }
      assertIncludes(r.text, "仅显示一次", "应展示一次性访问链接");
      assert(!r.navigations.includes("/kids/new"), "全程不应跳独立页");
    }],

    ["无控制台错误", async () => {
      const r = await mountKids();
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],
  ],
};

export default suite;
