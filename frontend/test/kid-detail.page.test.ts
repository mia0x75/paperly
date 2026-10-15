/**
 * test/kid-detail.page.test.ts -- kid detail page.
 *
 * Covers: read-only info, form prefill, full-table save payload, set as current,
 * this kid's papers, confirm-before-delete.
 */
import { assert, assertEqual, assertIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { selectText } from "./controls";
import { testWindow } from "./setup";
import KidDetailPage from "../src/pages/KidDetailPage.vue";
import type { Document } from "happy-dom";

const STUDENT = {
  id: 7,
  name: "小明",
  grade: 9,
  gender: 1,
  avatar: null,
  status: 1,
  created_at: "2026-09-01T08:00:00Z",
  updated_at: "2026-09-20T08:00:00Z",
};

const STUDENTS = [{ id: 7, name: "小明", grade: 9, status: 1 }];

const RESET_CODE = {
  kid_id: 7,
  access_code: "newcode1",
  kid_url: "/kid/newcode1",
};

async function mountDetail(overrides?: { get?: unknown; pin?: unknown }): Promise<MountResult> {
  return mountPage(KidDetailPage, {
    path: "/kids/7",
    responses: {
      "GET /api/kids/7": overrides?.get ?? STUDENT,
      "PUT /api/kids/7": STUDENT,
      "DELETE /api/kids/7": {},
      "POST /api/kids/7/access_code/reset": RESET_CODE,
      "POST /api/kids/7/pin/reset": overrides?.pin ?? { kid_id: 7, pin_set: true },
      "GET /api/kids": STUDENTS,
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

function setInputByPlaceholder(doc: Document, placeholder: string, value: string): void {
  const el = doc.querySelector(`input[placeholder="${placeholder}"]`);
  if (!el) throw new Error(`未找到输入框：${placeholder}`);
  (el as unknown as { value: string }).value = value;
  el.dispatchEvent(new testWindow.Event("input", { bubbles: true }));
}

function inputValue(doc: Document, type: string): string {
  const el = doc.querySelector(`input[type="${type}"]`);
  return el ? ((el as unknown as { value: string }).value ?? "") : "";
}

function jsonBody(r: MountResult, method: string, fragment: string): Record<string, unknown> {
  const c = [...r.calls].reverse().find((x) => x.method === method && x.url.includes(fragment));
  if (!c || c.body === null) throw new Error(`没有 ${method} ${fragment} 的 body`);
  return JSON.parse(c.body) as Record<string, unknown>;
}

const suite: TestSuite = {
  name: "KidDetailPage（查看 / 编辑 / 删除）",
  cases: [
    ["展示学生只读信息", async () => {
      const r = await mountDetail();
      assertIncludes(r.text, "小明", "姓名");
      assertIncludes(r.text, "初三", "年级中文（由 INTEGER 9 映射）");
      assertIncludes(r.text, "男", "性别中文");
      // no student-no/class/birthday/value-label columns exist; the page must not show these rows
      assert(!r.text.includes("学号"), "不应出现学号行");
      assert(!r.text.includes("生日"), "不应出现生日行");
    }],

    ["表单预填年级为下拉选中值", async () => {
      const r = await mountDetail();
      assertIncludes(selectText(r.doc, 0), "初三", "年级下拉预填 9 对应中文");
    }],

    ["保存回传完整载荷（防止整表 PUT 清空未传字段）", async () => {
      const r = await mountDetail();
      clickButton(r.doc, "保存");
      await flush();
      const body = jsonBody(r, "PUT", "/api/kids/7");
      assertEqual(body.name, "小明", "name");
      assertEqual(body.grade, 9, "grade 必须是 INTEGER 9");
      assertEqual(typeof body.grade, "number", "grade 类型");
      assertEqual(body.gender, 1, "gender");
      // fields the backend doesn't know must not be submitted
      assert(!("kid_no" in body), "不应提交 kid_no");
      assert(!("tags" in body), "不应提交 tags");
      assertIncludes(r.text, "已保存", "保存提示");
    }],

    ["未选年级时拦截保存", async () => {
      const r = await mountDetail({ get: { ...STUDENT, grade: 0 } });
      clickButton(r.doc, "保存");
      await flush();
      assertIncludes(r.text, "请选择年级", "年级校验");
      assertEqual(
        r.calls.filter((c) => c.method === "PUT").length,
        0,
        "不应发 PUT",
      );
    }],

    ["设为当前学生", async () => {
      const r = await mountDetail();
      clickButton(r.doc, "设为当前学生");
      await flush();
      // the button text changes from "Set as current" to "Current"; because Button
      // prepends an icon glyph, exact textContent matching is unreliable, so assert
      // "old text is gone" + persistence instead.
      assert(!r.text.includes("设为当前学生"), "按钮应切换为当前学生");
      assertEqual(testWindow.localStorage.getItem("paperly.selected_kid_id"), "7", "持久化选中");
    }],

    ["该生试卷跳转带 kid_id", async () => {
      const r = await mountDetail();
      clickButton(r.doc, "该生试卷");
      await flush();
      assert(
        r.navigations.includes("/exams?kid_id=7"),
        `应带该生过滤，实际 ${r.navigations.join(",")}`,
      );
    }],

    ["删除需二次确认，确认后 DELETE 并返回列表", async () => {
      const r = await mountDetail();
      clickButton(r.doc, "删除学生");
      await flush();
      assertIncludes(r.text, "确认删除「小明」？", "确认态含对象名");
      assertIncludes(r.text, "删除后不再显示，数据保留", "软删语义文案");
      assertEqual(r.calls.filter((c) => c.method === "DELETE").length, 0, "确认前不删");

      clickButton(r.doc, "确认删除");
      await flush();
      assertEqual(r.calls.filter((c) => c.method === "DELETE").length, 1, "确认后删除一次");
      assert(r.navigations.includes("/kids"), `应回列表，实际 ${r.navigations.join(",")}`);
    }],

    ["重置访问码：二次确认后展示新链接", async () => {
      const r = await mountDetail();
      clickButton(r.doc, "重置访问码");
      await flush();
      assertIncludes(r.text, "旧链接将立即失效", "确认弹窗");

      clickButton(r.doc, "确认重置");
      await flush();
      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/access_code/reset"));
      assert(post !== undefined, "应发重置请求");
      assertIncludes(r.text, "旧链接已失效", "重置后提示");
      const link = r.doc.querySelector("input[readonly]");
      assert(
        ((link as unknown as { value: string })?.value ?? "").includes("/kid/newcode1"),
        "应展示新链接",
      );
    }],

    ["重置访问码：取消不发请求", async () => {
      const r = await mountDetail();
      clickButton(r.doc, "重置访问码");
      await flush();
      clickButton(r.doc, "取消");
      await flush();
      assertEqual(
        r.calls.filter((c) => c.url.includes("/access_code/reset")).length,
        0,
        "取消不应发请求",
      );
    }],

    ["重置 PIN：填写则设置并提示已更新", async () => {
      const r = await mountDetail();
      clickButton(r.doc, "重置 PIN");
      await flush();
      setInputByPlaceholder(r.doc, "新 PIN（可留空）", "5678");
      clickButton(r.doc, "确认");
      await flush();
      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/pin/reset"));
      assert(post !== undefined, "应发重置 PIN 请求");
      assertEqual(JSON.parse(post?.body ?? "{}").new_pin, "5678", "提交新 PIN");
      assertIncludes(r.text, "PIN 已更新", "结果提示");
    }],

    ["重置 PIN：留空则清除并提示已清除", async () => {
      const r = await mountDetail({ pin: { kid_id: 7, pin_set: false } });
      clickButton(r.doc, "重置 PIN");
      await flush();
      clickButton(r.doc, "确认");
      await flush();
      const post = r.calls.find((c) => c.method === "POST" && c.url.includes("/pin/reset"));
      assertEqual(JSON.parse(post?.body ?? "{}").new_pin, "", "留空提交空串");
      assertIncludes(r.text, "PIN 已清除", "结果提示");
    }],

    ["加载失败显示错误且不崩", async () => {
      const r = await mountDetail({ get: { code: 40400, message: "学生不存在" } });
      assertIncludes(r.text, "学生不存在", "错误上屏");
      assertEqual(r.errors.length, 0, "不冒泡");
    }],

    ["无控制台错误", async () => {
      const r = await mountDetail();
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],
  ],
};

export default suite;
