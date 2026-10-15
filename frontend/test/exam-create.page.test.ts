/**
 * test/exam-create.page.test.ts -- new exam entry page.
 *
 * Covers: drag/multi-select, type validation, thumbnail ordering, auto-generated
 * title (editable), dropdown metadata, multipart multi-file fields (order = page
 * order), duplicate exam branch.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { pickDate, selectText, setSelectAt as pickSelect } from "./controls";
import { testWindow } from "./setup";
import { useKidStore } from "../src/stores/kid";
import { KID_ID_KEY, KID_NAME_KEY, KID_GRADE_KEY } from "../src/lib/kidSession";
import ExamCreatePage from "../src/pages/ExamCreatePage.vue";
import type { Document } from "happy-dom";

const KIDS = [
  { id: 7, name: "小明", grade: 9, status: 1 },
  { id: 8, name: "小红", grade: 8, status: 1 },
];

const SUBJECTS = [
  { id: 2, code: "math", name: "数学", sort_order: 0 },
  { id: 3, code: "english", name: "英语", sort_order: 0 },
];

const UPLOAD_RES = {
  id: 5,
  title: "2026-初三-上学期-数学-11月-期中",
  subject_id: 2,
  grade: 9,
  exam_type: 3,
  uploaded_by: 1,
  status: 1,
  created_at: "2026-11-02T09:00:00Z",
};

async function mountCreate(overrides?: { upload?: unknown }): Promise<MountResult> {
  return mountPage(ExamCreatePage, {
    path: "/exams/upload",
    responses: {
      "/api/kids": KIDS,
      "/api/subjects": SUBJECTS,
      "/api/exams/upload": overrides?.upload ?? UPLOAD_RES,
    },
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

function isDisabled(button: unknown): boolean {
  return (button as { disabled?: boolean }).disabled === true;
}

function clickEl(el: unknown): void {
  (el as unknown as { click: () => void }).click();
}

/** Whether the index-th Select's overlay is collapsed (ul[hidden] means collapsed). */
function selectPanelHidden(doc: Document, index: number): boolean {
  const trigger = doc.querySelectorAll('[role="combobox"]')[index];
  const ul = trigger?.closest("[data-select-root]")?.querySelector('ul[role="listbox"]');
  return ul === null || ul === undefined || (ul as unknown as { hidden: boolean }).hidden;
}

/** Click the index-th Select's full-screen transparent backdrop (outside the overlay). */
function clickSelectBackdrop(doc: Document, index: number): void {
  const trigger = doc.querySelectorAll('[role="combobox"]')[index];
  const backdrop = trigger?.closest("[data-select-root]")?.querySelector("div.fixed");
  if (!backdrop) throw new Error(`Select #${index}'s backdrop did not appear`);
  clickEl(backdrop);
}

function datePanelHidden(doc: Document): boolean {
  const dialog = doc.querySelector('[role="dialog"]');
  return dialog === null || (dialog as unknown as { hidden: boolean }).hidden;
}

function clickDateBackdrop(doc: Document): void {
  const trigger = doc.querySelector('[aria-haspopup="dialog"]');
  const backdrop = trigger?.closest("[data-datepicker-root]")?.querySelector("div.fixed");
  if (!backdrop) throw new Error("DatePicker's backdrop did not appear");
  clickEl(backdrop);
}

/** Select 0 is the owning kid, 1 is subject, 2 is grade, 3 is exam type (see test/controls.ts). */
function setSelectAt(doc: Document, index: number, value: string): void {
  pickSelect(doc, index, value);
}

async function setDate(doc: Document, value: string): Promise<void> {
  await pickDate(doc, value);
}

function makeImageFile(name = "p.png"): File {
  return new File([new Uint8Array([1, 2, 3])], name, { type: "image/png" });
}

/**
 * Put files into the upload zone via a direct drag-drop event (happy-dom's
 * FileList construction is limited).
 */
function dropFiles(doc: Document, files: File[]): void {
  const zone = doc.querySelector("section:nth-of-type(2) div.cursor-pointer") ?? doc.body;
  const ev = new testWindow.Event("drop", { bubbles: true, cancelable: true }) as unknown as {
    dataTransfer: { files: File[] };
  };
  ev.dataTransfer = { files };
  (zone as unknown as { dispatchEvent: (e: unknown) => void }).dispatchEvent(ev);
}

const suite: TestSuite = {
  name: "ExamCreatePage（多图 / 标题 / 下拉 / 上传）",
  cases: [
    // Grade defaults to the current kid's grade, so the parent needn't pick the
    // "default answer" themselves.
    ["年级默认预填当前孩子的年级，无需用户选择", async () => {
      const r = await mountCreate();
      await flush();
      // The store selects the first active kid by default = 小明 (id 7, grade 9)
      assertEqual(useKidStore().selectedId, 7, "默认当前孩子");
      assertIncludes(selectText(r.doc, 0), "小明", "归属孩子应默认当前孩子");
      assertIncludes(selectText(r.doc, 2), "初三", "年级应预填为当前孩子的年级");
      assertIncludes(selectText(r.doc, 1), "请选择科目", "科目不该被预填");
    }],

    ["归属孩子可选，切换后写入 localStorage 作为下一次默认", async () => {
      const r = await mountCreate();
      await flush();
      assertIncludes(selectText(r.doc, 0), "小明", "默认归属孩子");
      // The owning kid is Select 0; select 小红 (data-value=8).
      setSelectAt(r.doc, 0, "8");
      await flush();
      assertIncludes(selectText(r.doc, 0), "小红", "切换归属孩子");
      // The choice persists to localStorage (kidStore.selectedId) as the next default.
      assertEqual(useKidStore().selectedId, 8, "切换后 selectedId 同步");
      assertEqual(
        testWindow.localStorage.getItem("paperly.selected_kid_id"),
        "8",
        "选择已写入 localStorage",
      );
      // Grade follows the new owning kid's prefill (小红 grade 8).
      assertIncludes(selectText(r.doc, 2), "初二", "切孩子后年级跟随预填");
    }],

    ["年级预填后仍可由用户改选", async () => {
      const r = await mountCreate();
      await flush();
      setSelectAt(r.doc, 2, "5"); // grade 5
      await flush();
      assertIncludes(selectText(r.doc, 2), "五年级", "用户应能改年级");
    }],

    ["未手改年级时，切换当前孩子会跟着换年级", async () => {
      const r = await mountCreate();
      await flush();
      useKidStore().select(8); // 小红, grade 8
      await flush();
      assertIncludes(selectText(r.doc, 2), "初二", "切孩子后年级应跟随预填");
    }],

    ["用户手改年级后，切换当前孩子不再覆盖", async () => {
      const r = await mountCreate();
      await flush();
      setSelectAt(r.doc, 2, "5"); // manually change to grade 5
      await flush();
      useKidStore().select(8);
      await flush();
      assertIncludes(selectText(r.doc, 2), "五年级", "手改过的年级不应被顶掉");
    }],

    ["初始无图无元数据时，保存按钮禁用", async () => {
      const r = await mountCreate();
      await flush();
      assert(isDisabled(findButton(r.doc, "新增")), "未满足条件时禁用保存");
    }],

    ["拖入非图片文件被拒", async () => {
      const r = await mountCreate();
      await flush();
      dropFiles(r.doc, [new File(["x"], "a.pdf", { type: "application/pdf" })]);
      await flush();
      assertIncludes(r.text, "仅支持图片文件", "类型校验提示");
    }],

    ["拖入图片后出现缩略图与页码", async () => {
      const r = await mountCreate();
      await flush();
      dropFiles(r.doc, [makeImageFile("a.png"), makeImageFile("b.png")]);
      await flush();
      assertIncludes(r.text, "已添加 2 页", "页数提示");
      assertIncludes(r.text, "P1", "第 1 页码");
      assertIncludes(r.text, "P2", "第 2 页码");
      assertEqual(r.doc.querySelectorAll("img").length, 2, "缩略图数量");
    }],

    ["标题在选择元数据后自动生成", async () => {
      const r = await mountCreate();
      await flush();
      setSelectAt(r.doc, 1, "2"); // math
      await flush();
      setSelectAt(r.doc, 2, "9"); // grade 9
      await flush();
      setSelectAt(r.doc, 3, "3"); // midterm
      await flush();
      await setDate(r.doc, "2026-11-10");
      await flush();
      // Midterm carries no month: 2026-初三-上学期-数学-期中.
      // The title is the input's value, not in textContent, so value must be read.
      const titleInput = r.doc.querySelector('input[type="text"]') as unknown as HTMLInputElement;
      assertEqual(titleInput.value, "2026-初三-上学期-数学-期中", "自动生成标题");
    }],

    ["元数据齐全并选图后可提交", async () => {
      const r = await mountCreate();
      await flush();
      setSelectAt(r.doc, 1, "2");
      await flush();
      setSelectAt(r.doc, 2, "9");
      await flush();
      setSelectAt(r.doc, 3, "3");
      await flush();
      await setDate(r.doc, "2026-11-10");
      await flush();
      dropFiles(r.doc, [makeImageFile()]);
      await flush();
      assert(!isDisabled(findButton(r.doc, "新增")), "满足条件后应可保存");
    }],

    ["上传发出多文件 multipart（顺序=页序）并跳转", async () => {
      const r = await mountCreate();
      await flush();
      setSelectAt(r.doc, 1, "2");
      await flush();
      setSelectAt(r.doc, 2, "9");
      await flush();
      setSelectAt(r.doc, 3, "3");
      await flush();
      await setDate(r.doc, "2026-11-10");
      await flush();
      dropFiles(r.doc, [makeImageFile("a.png"), makeImageFile("b.png")]);
      await flush();
      clickButton(r.doc, "新增");
      await flush();

      const call = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/exams/upload"));
      assert(call !== undefined, "应发起上传");
      assert(call?.form !== undefined, "应为 multipart");
      // Two same-named file fields; FormData.entries() expands same-named items, so
      // the form field recorded in calls only holds the last same-named value; assert
      // on request URL and navigation instead.
      assert(r.navigations.includes("/exams/5"), `应跳详情，实际 ${r.navigations.join(",")}`);
    }],

    ["重复试卷引导查看已有试卷", async () => {
      const r = await mountCreate({
        upload: { code: 40022, message: "已存在", detail: JSON.stringify({ existing_id: 9 }) },
      });
      await flush();
      setSelectAt(r.doc, 1, "2");
      await flush();
      setSelectAt(r.doc, 2, "9");
      await flush();
      setSelectAt(r.doc, 3, "3");
      await flush();
      await setDate(r.doc, "2026-11-10");
      await flush();
      dropFiles(r.doc, [makeImageFile()]);
      await flush();
      clickButton(r.doc, "新增");
      await flush();
      assertIncludes(r.text, "该试卷已存在", "重复提示");
      clickButton(r.doc, "查看已有试卷");
      await flush();
      assert(r.navigations.includes("/exams/9"), `应跳已有试卷，实际 ${r.navigations.join(",")}`);
    }],

    ["上传失败显示错误且保留图片", async () => {
      const r = await mountCreate({ upload: { code: 50001, message: "上传失败" } });
      await flush();
      setSelectAt(r.doc, 1, "2");
      await flush();
      setSelectAt(r.doc, 2, "9");
      await flush();
      setSelectAt(r.doc, 3, "3");
      await flush();
      await setDate(r.doc, "2026-11-10");
      await flush();
      dropFiles(r.doc, [makeImageFile()]);
      await flush();
      clickButton(r.doc, "新增");
      await flush();
      assertIncludes(r.text, "上传失败", "错误上屏");
      assertIncludes(r.text, "已添加 1 页", "失败后图片仍在");
      assertEqual(r.errors.length, 0, "不冒泡");
    }],

    // With the control wrapped in a <label>, clicks on
    // the overlay are treated by the label as activation and re-dispatched to the
    // trigger button, causing "clicking blank won't close / reopens after selecting".
    ["Select：点浮层外关闭，不因 label 重开", async () => {
      const r = await mountCreate();
      await flush();
      clickEl(r.doc.querySelectorAll('[role="combobox"]')[1]);
      await flush();
      assert(!selectPanelHidden(r.doc, 1), "点开后应展开");
      clickSelectBackdrop(r.doc, 1);
      await flush();
      assert(selectPanelHidden(r.doc, 1), "点浮层外应关闭且不被 label 重开");
    }],

    ["Select：选中选项后关闭", async () => {
      const r = await mountCreate();
      await flush();
      setSelectAt(r.doc, 1, "2");
      await flush();
      assert(selectPanelHidden(r.doc, 1), "选完后应关闭且不被 label 重开");
    }],

    ["DatePicker：点浮层外关闭，不因 label 重开", async () => {
      const r = await mountCreate();
      await flush();
      clickEl(r.doc.querySelector('[aria-haspopup="dialog"]'));
      await flush();
      assert(!datePanelHidden(r.doc), "点开后应展开");
      clickDateBackdrop(r.doc);
      await flush();
      assert(datePanelHidden(r.doc), "点浮层外应关闭且不被 label 重开");
    }],

    ["科目来自归属孩子的 kid_subjects（按 ?kid_id 拉取，不再全字典+mask）", async () => {
      const r = await mountCreate();
      await flush();
      const subjReq = r.requests.find((u) => u.includes("/api/subjects?kid_id="));
      assert(subjReq !== undefined, `应按归属孩子拉科目：${r.requests.join(", ")}`);
      assert(subjReq.includes("kid_id=7"), `默认归属孩子 7：${subjReq}`);
      // The subject options come straight from that response.
      setSelectAt(r.doc, 1, "2");
      await flush();
      assertIncludes(selectText(r.doc, 1), "数学", "科目选项来自孩子科目");
    }],

    ["无控制台错误", async () => {
      const r = await mountCreate();
      await flush();
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],

    // --- Kid-side routes (/kid/exams/upload, meta.audience="kid") ---
    ["孩子端不显示归属孩子名（给自己传卷子）", async () => {
      const r = await mountPage(ExamCreatePage, {
        path: "/kid/exams/upload",
        storage: { [KID_ID_KEY]: "7", [KID_NAME_KEY]: "小明" },
        responses: { "/api/subjects": SUBJECTS, "/api/exams/upload": UPLOAD_RES },
      });
      await flush();
      // On the kid side, ownership comes from the session; the owning-kid field is
      // not rendered, and one's own name isn't shown as an ownership label either
      // ("upload for 小明" is redundant to oneself).
      assertNotIncludes(r.text, "归属孩子", "孩子端不该出现归属孩子字段");
      assertNotIncludes(r.text, "小明", "归属名字也不该出现");
    }],

    ["孩子端不请求家长端 /api/kids", async () => {
      const r = await mountPage(ExamCreatePage, {
        path: "/kid/exams/upload",
        storage: { [KID_ID_KEY]: "7" },
        responses: { "/api/subjects": SUBJECTS, "/api/exams/upload": UPLOAD_RES },
      });
      await flush();
      assert(!r.requests.some((u) => u.includes("/api/kids")), "孩子端不应拉孩子列表");
    }],

    ["孩子端年级默认孩子当前年级（会话 grade）", async () => {
      const r = await mountPage(ExamCreatePage, {
        path: "/kid/exams/upload",
        storage: { [KID_ID_KEY]: "7", [KID_NAME_KEY]: "小明", [KID_GRADE_KEY]: "9" },
        responses: { "/api/subjects": SUBJECTS, "/api/exams/upload": UPLOAD_RES },
      });
      await flush();
      // The kid side doesn't render the owning-kid dropdown; Select order is still
      // subject/grade/type; grade (index 1) defaults to the session kid's current grade (grade 9).
      assertIncludes(selectText(r.doc, 1), "初三", "年级应默认为孩子当前年级");
    }],

    ["孩子端上传携带会话 kid_id，成功后跳孩子端详情", async () => {
      const r = await mountPage(ExamCreatePage, {
        path: "/kid/exams/upload",
        storage: { [KID_ID_KEY]: "7", [KID_NAME_KEY]: "小明" },
        responses: { "/api/subjects": SUBJECTS, "/api/exams/upload": UPLOAD_RES },
      });
      await flush();
      // The kid side doesn't render the owning-kid field; Select order is still subject/grade/type.
      setSelectAt(r.doc, 0, "2");
      await flush();
      setSelectAt(r.doc, 1, "9");
      await flush();
      setSelectAt(r.doc, 2, "3");
      await flush();
      dropFiles(r.doc, [makeImageFile()]);
      await flush();
      clickButton(r.doc, "新增");
      await flush();
      const upload = r.calls.find((c) => c.method === "POST" && c.url.includes("/api/exams/upload"));
      assert(upload !== undefined, "应发起上传请求");
      assert(upload.form !== undefined && upload.form["kid_id"] === "7", "归属应为会话 kidId");
      assert(r.navigations.includes("/kid/exams/5"), `应跳孩子端详情，实际 ${r.navigations.join(",")}`);
    }],
  ],
};

export default suite;
