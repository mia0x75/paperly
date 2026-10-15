/**
 * test/exam-viewer.page.test.ts -- exam view/edit page.
 *
 * Covers: page-image gallery paging, read-only info, whole-record save payload,
 * two-step delete confirmation with navigation.
 */
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import { flush, mountPage, type MountResult } from "./mount";
import { setNativeInput, setSelectAt as pickSelect } from "./controls";
import { testWindow } from "./setup";
import ExamViewerPage from "../src/pages/ExamViewerPage.vue";
import type { Document } from "happy-dom";

const EXAM = {
  id: 5,
  title: "2024-初三-上学期-数学-11月-期中",
  subject_id: 2,
  grade: 9,
  exam_type: 3,
  exam_date: "2024-11-01",
  kid_id: 7,
  uploaded_by: 1,
  status: 1,
  page_count: 2,
  pages: [
    { id: 11, page_index: 1, file_path: "/x/p1.png", sha256: "aa" },
    { id: 12, page_index: 2, file_path: "/x/p2.png", sha256: "bb" },
  ],
  created_at: "2024-11-02T09:00:00Z",
  updated_at: "2024-11-02T09:00:00Z",
};

const SUBJECTS = [
  { id: 2, code: "math", name: "数学", sort_order: 0 },
  { id: 3, code: "english", name: "英语", sort_order: 0 },
];

const KIDS = [
  { id: 7, name: "小明", grade: 9, status: 1 },
  { id: 8, name: "小红", grade: 8, status: 1 },
];

/**
 * Response table of the latest mountViewer, referenced by closure. Cases can
 * rewrite it mid-test to simulate server data changing during polling;
 * stubFetch re-reads Object.entries per request, so a rewrite applies to all
 * subsequent requests.
 */
let viewerResponses: Record<string, unknown> = {};

async function mountViewer(overrides?: {
  exam?: unknown;
  kids?: unknown;
  /** Extra response entries (e.g. a method-specific error envelope). */
  extra?: Record<string, unknown>;
}): Promise<MountResult> {
  viewerResponses = {
    // extra goes first: stubFetch matches in insertion order, so method-qualified
    // keys ("POST /api/exams/5/pages/ops") must be checked before bare path keys
    // ("/api/exams/5" matches any method).
    ...(overrides?.extra ?? {}),
    "/api/exams/5": overrides?.exam ?? EXAM,
    "/api/subjects": SUBJECTS,
    "/api/kids": overrides?.kids ?? KIDS,
  };
  return mountPage(ExamViewerPage, { path: "/exams/5", responses: viewerResponses });
}

/**
 * happy-dom and lib.dom node types differ; collapse to a minimal parentElement
 * chain before structural assertions.
 */
type ParentTree = { parentElement: ParentTree | null };

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

/** Click the editor-card control with this title (move up/down/delete the page). */
function clickTitle(doc: Document, title: string, index = 0): void {
  const el = doc.querySelectorAll(`button[title="${title}"]`)[index];
  if (!el) throw new Error(`未找到 title="${title}" 的第 ${index} 个按钮`);
  (el as unknown as { click: () => void }).click();
}

function titleCount(doc: Document, title: string): number {
  return doc.querySelectorAll(`button[title="${title}"]`).length;
}

/** sign builds one signed summary segment marker (mirrors the backend's stored shape). */
function sign(sig: string, type: number, id: number, text: string): string {
  return `<div class="summary-sign" data-sig="${sig}" data-author-type="${type}" data-author="${id}">${text}</div>`;
}

/**
 * Simulate file selection: happy-dom's input.files is read-only, so swap the
 * instance property in place.
 */
function addFile(doc: Document, name: string): void {
  const input = doc.querySelector('input[type="file"]') as unknown as HTMLInputElement | null;
  if (!input) throw new Error("未找到文件输入框");
  const file = new File(["x"], name, { type: "image/png" });
  Object.defineProperty(input, "files", { value: [file], configurable: true });
  // Must use happy-dom's Event: Bun's native Event occupies the global name
  // (setup doesn't override it) and happy-dom's dispatchEvent doesn't recognize it.
  // At the type level happy-dom Event and lib.dom Event differ structurally, so it
  // is closed off via unknown (same instance at runtime).
  input.dispatchEvent(new testWindow.Event("change") as unknown as Event);
}

function setSelectAt(doc: Document, index: number, value: string): void {
  pickSelect(doc, index, value);
}

function lastBody(r: MountResult, method: string): Record<string, unknown> {
  const call = [...r.calls].reverse().find((c) => c.method === method);
  if (!call || call.body === null) throw new Error(`没有 ${method} 请求体`);
  return JSON.parse(call.body) as Record<string, unknown>;
}

const suite: TestSuite = {
  name: "ExamViewerPage（查看 / 编辑 / 删除）",
  cases: [
    ["基本信息里归属孩子显示名字而非 #id", async () => {
      const r = await mountViewer();
      await flush();
      assertIncludes(r.text, "小明", "归属应显示孩子名");
      assertNotIncludes(r.text, "学生 #7", "不应再显示裸 id");
    }],

    ["归属孩子查不到时退回「学生 #id」", async () => {
      const r = await mountViewer({ exam: { ...EXAM, kid_id: 999 } });
      await flush();
      assertIncludes(r.text, "学生 #999", "查不到孩子时应退回 #id");
    }],

    ["未归属的试卷显示「未归属」", async () => {
      const r = await mountViewer({ exam: { ...EXAM, kid_id: null } });
      await flush();
      assertIncludes(r.text, "未归属", "无归属应显示未归属");
    }],

    [
      "孩子端查看：不拉 /api/kids，归属显示「我」，不被强踢登录",
      async () => {
        const r = await mountPage(ExamViewerPage, {
          path: "/kid/exams/5",
          storage: {
            "paperly.kid_token": "jwt-kid",
            "paperly.kid_name": "小明",
            "paperly.kid_id": "7",
          },
          responses: {
            "/api/exams/5": EXAM,
            "/api/subjects": SUBJECTS,
          },
        });
        await flush();
        // A kid token hitting /api/kids (requireParent) gets 40100, which clears the
        // kid session and navigates to /kid/login; the kid side must not call it.
        assert(
          !r.requests.some((u) => u.includes("/api/kids")),
          `孩子端不应请求 /api/kids，实际：${r.requests.join(", ")}`,
        );
        // The kid side titles with "我的试卷"; the owning-kid row shows "我" rather
        // than `学生 #7` from an unresolvable kid list (the kid side has no
        // /api/kids data).
        assertIncludes(r.text, "我的试卷：", "孩子端标题");
        assertIncludes(r.text, "2024-初三-上学期-数学-11月-期中", "试卷正常加载");
        const ownerRow = Array.from(r.doc.querySelectorAll("dl div")).find(
          (d) => d.querySelector("dt")?.textContent === "归属孩子",
        );
        if (!ownerRow) throw new Error("应有归属孩子行");
        assertEqual(ownerRow.querySelector("dd")?.textContent, "我", "归属对孩子显示「我」");
        assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
      },
    ],

    ["展示试卷只读信息与标题", async () => {
      const r = await mountViewer();
      await flush();
      assertIncludes(r.text, "2024-初三-上学期-数学-11月-期中", "标题");
      assertIncludes(r.text, "数学", "科目名");
      assertIncludes(r.text, "初三", "年级中文");
      assertIncludes(r.text, "期中", "考试类型");
      assertIncludes(r.text, "2024-11-01", "考试日期");
      assertIncludes(r.text, "小明", "归属孩子名");
      assertIncludes(r.text, "2 张图片", "页数");
    }],

    ["返回列表与编辑/删除同排居最左，归属显示为胶囊「XX」", async () => {
      const r = await mountViewer();
      await flush();
      assertIncludes(r.text, "小明", "归属胶囊文案");
      assert(!r.text.includes("正在查看"), "旧句式不应再出现");
      const back = findButton(r.doc, "返回列表") as unknown as ParentTree;
      const action = findButton(r.doc, "编辑") as unknown as ParentTree;
      assertEqual(
        back.parentElement?.parentElement ?? null,
        action.parentElement?.parentElement ?? null,
        "返回列表与动作行应同网格（视觉同一行）",
      );
      // DOM order: "back to list" must appear before the action row (leftmost)
      assert(
        (back as unknown as { compareDocumentPosition: (o: unknown) => number })
          .compareDocumentPosition(action) & 4,
        "返回列表应在动作行之前",
      );
      const pill = Array.from(r.doc.querySelectorAll("span")).find(
        (x) => (x.textContent ?? "").trim() === "小明",
      );
      assert(pill !== undefined, "归属胶囊存在");
      const cls = pill.getAttribute("class") ?? "";
      assert(cls.includes("rounded-full"), "胶囊应全圆角");
      assert(/bg-/.test(cls), "胶囊应有背景色");
    }],

    ["录入时间格式化为 yyyy-MM-dd hh:mm:ss，原始 ISO 不上屏", async () => {
      const r = await mountViewer();
      await flush();
      assert(!r.text.includes("2024-11-02T09:00:00Z"), "RFC3339 原始值不应上屏");
      assert(
        /\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/.test(r.text),
        "应为 yyyy-MM-dd hh:mm:ss 格式",
      );
    }],

    ["动作行与信息卡同网格，「编辑试卷页」与缩略图同行", async () => {
      const r = await mountViewer();
      await flush();
      // The action row (edit/delete exam) and the info card must be subtrees of the same
      // grid: only then is width set by the same column and both edges aligned.
      const action = findButton(r.doc, "编辑") as unknown as ParentTree;
      const grid = action.parentElement?.parentElement ?? null;
      const card = Array.from(r.doc.querySelectorAll("section")).find((x) =>
        (x.textContent ?? "").includes("试卷基本信息"),
      );
      assert(card !== undefined, "信息卡存在");
      const cardGrid = (card as unknown as ParentTree).parentElement?.parentElement ?? null;
      assertEqual(cardGrid, grid, "信息卡与动作行应同属一个网格");
      // "Edit exam pages" shares a row with the thumbnails: same parent container (same flex row).
      const editPages = findButton(r.doc, "编辑试卷页") as Element;
      assert(
        editPages.parentElement?.querySelector('img[alt="P1"]') !== null,
        "编辑试卷页按钮的行内应含缩略图",
      );
    }],

    ["画廊展示页图并显示页码", async () => {
      const r = await mountViewer();
      await flush();
      assertIncludes(r.text, "第 1 / 2 页", "页码");
      const imgs = r.doc.querySelectorAll("img");
      // Large image + 2 thumbnails
      assert(imgs.length >= 3, `应渲染大图与缩略图，实际 ${imgs.length}`);
      const big = r.doc.querySelector('img[alt^="第"]') as unknown as HTMLImageElement | null;
      assert(big !== null, "应有大图（字节拉取完成后呈现）");
      // The page-image endpoint is behind the gate but <img> can't carry
      // Authorization, so bytes are fetched on demand and presented as a blob
      // URL, with credentials only in request headers.
      assert((big?.getAttribute("src") ?? "").startsWith("blob:"), "大图经 blob URL 呈现");
      assert(
        r.requests.some((u) => u.includes("/api/pages/11/file")),
        `应拉取页图字节：${r.requests.join(", ")}`,
      );
    }],

    ["下一页切到第 2 页", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "下一页");
      await flush();
      assertIncludes(r.text, "第 2 / 2 页", "翻到第 2 页");
    }],

    // --- Zoomed viewing (page images are illegible in-page, need zooming) ---
    ["默认不进放大层，点「放大」打开", async () => {
      const r = await mountViewer();
      await flush();
      assertEqual(r.doc.querySelector('[data-testid="exam-zoom"]'), null, "初始无放大层");
      clickButton(r.doc, "放大");
      await flush();
      assert(r.doc.querySelector('[data-testid="exam-zoom"]') !== null, "点「放大」应打开");
      const fitBtn = (label: string) =>
        r.doc.querySelector(`[data-testid="exam-zoom"] button[aria-label="${label}"]`);
      assert(fitBtn("适应高度") !== null, "应有适应高度入口");
      assert(fitBtn("适应宽度") !== null, "应有适应宽度入口");
      assertIncludes(r.text, "100%", "打开时是 100%（适应当前轴）");
    }],

    ["放大层有真实倍数档位，且能逐档放大/缩小", async () => {
      // The core of this item: not "fullscreen" but **zoom** -- there must be an adjustable scale.
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();

      const zoomBtn = (label: string) =>
        r.doc.querySelector(`[data-testid="exam-zoom"] button[aria-label="${label}"]`);
      assert(zoomBtn("放大") !== null, "应有放大按钮");
      assert(zoomBtn("缩小") !== null, "应有缩小按钮");

      // At 100% "zoom out" must be disabled, or it could go below fit-to-window
      // (smaller and smaller)
      assertEqual(
        (zoomBtn("缩小") as unknown as { disabled?: boolean }).disabled,
        true,
        "100%（适应窗口）时不能再缩小",
      );

      (zoomBtn("放大") as unknown as { click: () => void }).click();
      await flush();
      assertIncludes(r.text, "150%", "放大一档到 150%");
      (zoomBtn("放大") as unknown as { click: () => void }).click();
      await flush();
      assertIncludes(r.text, "200%", "再放大到 200%");
      (zoomBtn("缩小") as unknown as { click: () => void }).click();
      await flush();
      assertIncludes(r.text, "150%", "缩小回 150%");
    }],

    ["放大到上限后「放大」禁用（不会无限放大）", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const zoomBtn = r.doc.querySelector('[data-testid="exam-zoom"] button[aria-label="放大"]') as unknown as {
        click: () => void;
        disabled?: boolean;
      };
      for (let i = 0; i < 10; i++) {
        if (zoomBtn.disabled === true) break;
        zoomBtn.click();
        await flush();
      }
      assertEqual(zoomBtn.disabled, true, "到上限后应禁用");
      assertIncludes(r.text, "400%", "上限为 400%");
    }],

    ["适应高度/适应宽度把倍数复位回 100%", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const zoomBtn = r.doc.querySelector('[data-testid="exam-zoom"] button[aria-label="放大"]') as unknown as {
        click: () => void;
      };
      zoomBtn.click();
      await flush();
      zoomBtn.click();
      await flush();
      assertIncludes(r.text, "200%", "先放大");
      const fitwBtn = r.doc.querySelector('[data-testid="exam-zoom"] button[aria-label="适应宽度"]') as unknown as {
        click: () => void;
      };
      fitwBtn.click();
      await flush();
      assertIncludes(r.text, "100%", "适应宽度应复位回 100%");
      clickButton(r.doc, "放大");
      await flush();
      const fithBtn = r.doc.querySelector('[data-testid="exam-zoom"] button[aria-label="适应高度"]') as unknown as {
        click: () => void;
      };
      fithBtn.click();
      await flush();
      assertIncludes(r.text, "100%", "适应高度应复位回 100%");
    }],

    ["放大层内翻页，关闭后画廊停在同一页（共用 pageIndex）", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const next = r.doc.querySelector('[data-testid="exam-zoom"] button[aria-label="下一页"]');
      assert(next !== null, "放大层应有下一页按钮");
      (next as unknown as { click: () => void }).click();
      await flush();
      assertIncludes(r.text, "第 2 / 2 页", "放大层内翻到第 2 页");
      const closeBtn = r.doc.querySelector('[data-testid="exam-zoom"] button[aria-label="关闭"]') as unknown as {
        click: () => void;
        disabled?: boolean;
      };
      closeBtn.click();
      await flush();
      assertEqual(r.doc.querySelector('[data-testid="exam-zoom"]'), null, "关闭后放大层消失");
      assertIncludes(r.text, "第 2 / 2 页", "关闭后画廊停在同一页");
    }],

    ["键盘：+/- 缩放、0 复位、Esc 关闭、← → 翻页", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const win = r.doc.defaultView!;
      const key = (k: string) => win.dispatchEvent(new win.KeyboardEvent("keydown", { key: k, bubbles: true }));

      key("+");
      await flush();
      assertIncludes(r.text, "150%", "+ 应放大");
      key("+");
      await flush();
      assertIncludes(r.text, "200%", "+ 应继续放大");
      key("-");
      await flush();
      assertIncludes(r.text, "150%", "- 应缩小");
      key("0");
      await flush();
      assertIncludes(r.text, "100%", "0 应复位到适应窗口");

      key("ArrowRight");
      await flush();
      assertIncludes(r.text, "第 2 / 2 页", "→ 应翻到下一页");
      key("ArrowLeft");
      await flush();
      assertIncludes(r.text, "第 1 / 2 页", "← 应翻回上一页");

      key("Escape");
      await flush();
      assertEqual(r.doc.querySelector('[data-testid="exam-zoom"]'), null, "Esc 应关闭放大层");
    }],

    ["不在放大层时 +/- 与方向键不劫持页面", async () => {
      const r = await mountViewer();
      await flush();
      const win = r.doc.defaultView!;
      for (const k of ["+", "-", "0", "ArrowRight", "ArrowLeft"]) {
        win.dispatchEvent(new win.KeyboardEvent("keydown", { key: k, bubbles: true }));
      }
      await flush();
      assertIncludes(r.text, "第 1 / 2 页", "非放大状态下快捷键应无效果");
    }],

    ["无页面时「放大」禁用", async () => {
      const r = await mountViewer({ exam: { ...(EXAM as Record<string, unknown>), pages: [], page_count: 0 } });
      await flush();
      const btn = findButton(r.doc, "放大") as unknown as { disabled?: boolean };
      assertEqual(btn.disabled, true, "没有页图时不应能放大");
      clickButton(r.doc, "放大");
      await flush();
      assertEqual(r.doc.querySelector('[data-testid="exam-zoom"]'), null, "无页时不应打开放大层");
    }],

    ["图片尺寸用显式宽高而非 transform（滚动范围才对）", async () => {
      // transform:scale() doesn't change the layout box size; overflow-auto computes
      // the scroll range from the original size, so after zooming the right/bottom
      // half is clipped and unreachable. This locks in "has concrete width/height".
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const img = r.doc.querySelector('[data-testid="zoom-image"]') as unknown as HTMLImageElement | null;
      assert(img !== null, "放大层应有页图");
      const style = img?.getAttribute("style") ?? "";
      assert(!style.includes("transform"), "不得用 transform: scale 放大");
      assert(style.includes("max-width"), "图片未 load 时退回 CSS 适应窗口");
    }],

    ["放大图必须带 max-w-none 抵消 Tailwind preflight 的 max-width:100%", async () => {
      // A pitfall measured in a real browser: preflight's `img { max-width: 100% }`
      // clamps the explicit zoomed width back to the container width -- at 200% the
      // image doesn't widen and gets squashed (a 1152×1358 square image). This pins
      // max-w-none so nobody deletes it while "cleaning up unused class names".
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const img = r.doc.querySelector('[data-testid="zoom-image"]') as unknown as HTMLElement | null;
      assert(img !== null, "放大层应有页图");
      assert(
        (img?.className ?? "").includes("max-w-none"),
        `放大图需 max-w-none，实际 class="${img?.className}"`,
      );
      // Also give only width, not height: leave the aspect ratio to the browser,
      // avoiding distortion from pinning it
      clickButton(r.doc, "放大");
      await flush();
      const style = img?.getAttribute("style") ?? "";
      assert(/\bwidth\s*:/.test(style), "应显式给宽度");
      // The \bwidth boundary matters: the fallback branch's `max-height:` also
      // contains "height", so includes("height:") would misfire.
      assert(!/(^|[\s;{])height\s*:/.test(style), "不应显式给 height（宽高比交给浏览器，避免钉死拉变形）");
    }],

    // --- Zoom layer reachability ---
    ["图片用 m-auto 居中而非 flex 居中，溢出方向才滚得到", async () => {
      // A bug measured in real Chromium: under `items-center justify-center` at 200%,
      // the image's top is clipped 389px outside the container while scrollTop is
      // already 0 -- the header is never visible, and scrollHeight doesn't even count
      // the clipped part. Only auto margins make both directions reachable.
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const box = r.doc.querySelector('[data-testid="zoom-box"]') as unknown as HTMLElement;
      const img = r.doc.querySelector('[data-testid="zoom-image"]') as unknown as HTMLElement;
      const boxCls = box?.className ?? "";
      assert(!/items-center/.test(boxCls), `容器不得用 items-center（会把溢出甩到起始边外），实际 "${boxCls}"`);
      assert(!/justify-center/.test(boxCls), `容器不得用 justify-center（同上），实际 "${boxCls}"`);
      assert(/items-start/.test(boxCls), "容器应为 items-start（配合 auto margin 居中）");
      assert(
        /(^|\s)m-auto(\s|$)/.test(img?.className ?? ""),
        `图片需 m-auto（auto margin 只在有富余空间时居中），实际 "${img?.className}"`,
      );
    }],

    // --- Wheel zoom + drag pan ---
    ["滚轮可缩放（向上放大、向下缩小）", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const box = r.doc.querySelector('[data-testid="zoom-box"]') as unknown as HTMLElement;
      const img = r.doc.querySelector('[data-testid="zoom-image"]') as unknown as HTMLElement;
      // happy-dom has no layout, so getBoundingClientRect is all 0; zoom needs the
      // image to have a size to compensate scroll
      img.getBoundingClientRect = () => ({ left: 0, top: 0, width: 400, height: 600 }) as DOMRect;
      const win = r.doc.defaultView!;
      const wheel = (dy: number) =>
        box.dispatchEvent(
          new win.WheelEvent("wheel", { deltaY: dy, bubbles: true, cancelable: true }) as unknown as Event,
        );
      wheel(-1);
      await flush();
      assertIncludes(r.text, "150%", "滚轮向上应放大一档");
      wheel(-1);
      await flush();
      assertIncludes(r.text, "200%", "继续向上应再放大");
      wheel(1);
      await flush();
      assertIncludes(r.text, "150%", "滚轮向下应缩小一档");
      wheel(99);
      await flush();
      assertIncludes(r.text, "100%", "滚轮向下到底应为 100%");
    }],

    ["滚轮缩放锚定光标：不会把正在看的位置甩掉", async () => {
      // Changing only the scale would zoom about the container's top-left and the
      // line the parent is watching would drift off. The component compensates
      // scroll by the cursor's relative position within the image; this verifies
      // the compensation direction with a mock rect.
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const box = r.doc.querySelector('[data-testid="zoom-box"]') as unknown as HTMLElement;
      const img = r.doc.querySelector('[data-testid="zoom-image"]') as unknown as HTMLElement;
      let sl = 0;
      Object.defineProperty(box, "scrollLeft", {
        get: () => sl,
        set: (v: number) => {
          sl = v;
        },
        configurable: true,
      });
      const win = r.doc.defaultView!;
      img.getBoundingClientRect = () => ({ left: 100, top: 200, width: 200, height: 300 }) as DOMRect;
      // happy-dom's WheelEvent doesn't take clientX/clientY (measured undefined), so set them after
      const ev = new win.WheelEvent("wheel", { deltaY: -1, bubbles: true, cancelable: true });
      Object.defineProperty(ev, "clientX", { value: 200 });
      Object.defineProperty(ev, "clientY", { value: 350 });
      // Cursor at the image center -> after zoom the image's top-left shifts
      // left/up, so the scroll delta must be > 0
      box.dispatchEvent(ev as unknown as Event);
      await flush();
      assert(sl > 0, `放大时光标下的点应保持在原位（scrollLeft 应补偿为正），实际 ${sl}`);
    }],

    ["按住可拖动平移，松开恢复 grab 光标", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      const box = r.doc.querySelector('[data-testid="zoom-box"]') as unknown as HTMLElement;
      const win = r.doc.defaultView!;
      let sl = 0;
      let st = 0;
      Object.defineProperty(box, "scrollLeft", {
        get: () => sl,
        set: (v: number) => {
          sl = v;
        },
        configurable: true,
      });
      Object.defineProperty(box, "scrollTop", {
        get: () => st,
        set: (v: number) => {
          st = v;
        },
        configurable: true,
      });
      // happy-dom's PointerEvent is incompatible with DOM lib's Event type (missing
      // isTrusted etc.), bridged via unknown rather than any.
      const pointer = (type: string, init: { clientX?: number; clientY?: number }): void => {
        box.dispatchEvent(new win.PointerEvent(type, { bubbles: true, ...init }) as unknown as Event);
      };
      pointer("pointerdown", { clientX: 100, clientY: 100 });
      await flush();
      assert(/cursor-grabbing/.test(box.className ?? ""), `按下应变 grabbing，实际 "${box.className}"`);
      // Drag down-left -> the viewport follows, scroll amount grows
      pointer("pointermove", { clientX: 60, clientY: 40 });
      await flush();
      assertEqual(sl, 40, `横向拖 40px 应滚动 40，实际 ${sl}`);
      assertEqual(st, 60, `纵向拖 60px 应滚动 60，实际 ${st}`);
      pointer("pointerup", {});
      await flush();
      assert(/cursor-grab(?!bing)/.test(box.className ?? ""), "松开应恢复 grab 光标");
      // Moving after release should not keep scrolling
      const after = sl;
      pointer("pointermove", { clientX: 200, clientY: 200 });
      await flush();
      assertEqual(sl, after, "松开后拖动不应再滚动");
    }],

    ["切换适应轴后仍停在 100%，且当前轴有高亮标记", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "放大");
      await flush();
      // Highlight is judged by the secondary variant (bg-secondary): can't use
      // bg-accent, since the ghost variant carries hover:bg-accent and would be
      // misread as highlighted.
      const active = (el: HTMLElement) => /(^|\s)bg-secondary(\s|$)/.test(el.className ?? "");
      const w = r.doc.querySelector('[data-testid="fit-width"]') as unknown as HTMLElement;
      const h = r.doc.querySelector('[data-testid="fit-height"]') as unknown as HTMLElement;
      assert(active(h), "默认适应高度应高亮");
      (w as unknown as { click: () => void }).click();
      await flush();
      assertIncludes(r.text, "100%", "换适应轴后倍数回到 100%");
      assert(active(w), "切到适应宽度后应高亮它");
      assert(!active(h), "适应高度此时不应高亮");
    }],

    ["编辑弹层默认关闭，点顶栏「编辑」才打开", async () => {
      const r = await mountViewer();
      await flush();
      assertNotIncludes(r.text, "试卷名称", "初始不应有编辑弹层");
      clickButton(r.doc, "编辑");
      await flush();
      assertIncludes(r.text, "试卷名称", "点「编辑」应弹出编辑层（弹层表单标记）");
    }],

    ["「取消」关闭编辑弹层且不发 PUT", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      clickButton(r.doc, "取消");
      await flush();
      assertNotIncludes(r.text, "试卷名称", "取消后弹层应关闭");
      assertEqual(r.calls.filter((c) => c.method === "PUT").length, 0, "取消不应保存");
    }],

    ["保存成功后自动关闭弹层，提示留在页面顶栏", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      clickButton(r.doc, "保存");
      await flush();
      assertNotIncludes(r.text, "试卷名称", "保存成功后弹层应关闭");
      assertIncludes(r.text, "已保存", "提示移到顶栏，关弹层后仍可见");
    }],

    ["保存回传完整载荷（防止整表 PUT 清空未传字段）", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      clickButton(r.doc, "保存");
      await flush();
      const body = lastBody(r, "PUT");
      assertEqual(body.title, "2024-初三-上学期-数学-11月-期中", "title");
      assertEqual(body.subject_id, 2, "subject_id");
      assertEqual(body.grade, 9, "grade");
      assertEqual(body.exam_type, 3, "exam_type");
      assertEqual(body.exam_date, "2024-11-01", "exam_date");
      assertEqual(body.kid_id, 7, "kid_id 保持");
      assertIncludes(r.text, "已保存", "保存成功提示");
    }],

    ["切换科目后保存为新 id", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      setSelectAt(r.doc, 1, "3"); // Select 1 is subject -> english (0th is owning kid)
      await flush();
      clickButton(r.doc, "保存");
      await flush();
      assertEqual(lastBody(r, "PUT").subject_id, 3, "科目切换");
    }],

    ["删除需二次确认，确认后 DELETE 并原地刷新为已删除态", async () => {
      // Use a mutable object to simulate server state changing after delete:
      // initially active, flipped to deleted before confirming; after delete,
      // remove() re-GETs and gets the deleted state.
      const examObj = { ...EXAM };
      const r = await mountViewer({ exam: examObj });
      await flush();
      clickButton(r.doc, "删除试卷");
      await flush();
      assertIncludes(r.text, "确认删除", "确认态");
      assertEqual(r.calls.filter((c) => c.method === "DELETE").length, 0, "确认前不删");

      examObj.status = 2;
      clickButton(r.doc, "确认删除");
      await flush();
      assertEqual(r.calls.filter((c) => c.method === "DELETE").length, 1, "确认后删除一次");
      assertEqual(r.navigations.length, 0, "删除后不跳转");
      assertIncludes(r.text, "已删除", "状态徽章变「已删除」");
      assertIncludes(r.text, "恢复试卷", "按钮切为「恢复试卷」");
      assertNotIncludes(r.text, "删除试卷", "不再显示删除按钮");
    }],

    // Deleted exam: shows "restore exam" instead of "delete exam"; clicking pops a
    // two-step confirmation, and on confirm POSTs restore, the status badge returns
    // to normal and the button switches back to "delete exam".
    ["已删除试卷显示「恢复试卷」，确认后恢复并原地刷新", async () => {
      const r = await mountViewer({
        exam: { ...EXAM, status: 2 },
        extra: {
          "POST /api/exams/5/restore": { ...EXAM, status: 1 },
        },
      });
      await flush();
      assertIncludes(r.text, "恢复试卷", "已删态显示恢复按钮");
      assertNotIncludes(r.text, "删除试卷", "已删态不显示删除按钮");

      clickButton(r.doc, "恢复试卷");
      await flush();
      assertIncludes(r.text, "确认恢复", "确认态");
      assertEqual(
        r.calls.filter((c) => c.method === "POST" && c.url.includes("/restore")).length,
        0,
        "确认前不恢复",
      );

      clickButton(r.doc, "确认恢复");
      await flush();
      assertEqual(
        r.calls.filter((c) => c.method === "POST" && c.url.includes("/api/exams/5/restore")).length,
        1,
        "确认后恢复一次",
      );
      assertIncludes(r.text, "正常", "状态徽章回「正常」");
      assertIncludes(r.text, "删除试卷", "恢复后按钮切回「删除试卷」");
      assertNotIncludes(r.text, "恢复试卷", "恢复后不再显示恢复按钮");
    }],

    ["加载失败显示错误且不崩", async () => {
      const r = await mountViewer({ exam: { code: 40401, message: "试卷不存在" } });
      assertIncludes(r.text, "试卷不存在", "错误上屏");
      assertEqual(r.errors.length, 0, "不冒泡");
    }],

    // The edit dialog's subject/grade/type must not offer a "not set" option.
    ["编辑弹层无「未设置」选项，标题/科目/年级/类型均标必填", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      assertNotIncludes(r.text, "未设置", "不应出现「未设置」");
      const markers = r.doc.querySelectorAll(".text-destructive");
      assert(markers.length >= 4, `标题/科目/年级/类型应有 4 个必填星号，实际 ${markers.length}`);
    }],

    // Save is disabled when required fields are incomplete, enabled once filled.
    ["必填未齐全时「保存」禁用", async () => {
      const r = await mountViewer({
        exam: { ...EXAM, title: "", subject_id: null, grade: null, exam_type: null },
      });
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      const save = findButton(r.doc, "保存") as unknown as { disabled?: boolean };
      assertEqual(save.disabled, true, "缺必填时应禁用保存");
    }],

    ["填齐必填后「保存」可用并提交非空字段", async () => {
      const r = await mountViewer({
        exam: { ...EXAM, title: "", subject_id: null, grade: null, exam_type: null },
      });
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      // After manually filling the title, fill in subject/grade/type in turn.
      const titleInput = r.doc.querySelector('input[type="text"]') as unknown as HTMLInputElement;
      setNativeInput(titleInput, "补测卷");
      await flush();
      setSelectAt(r.doc, 1, "2"); // subject (0th is owning kid)
      await flush();
      setSelectAt(r.doc, 2, "9"); // grade
      await flush();
      setSelectAt(r.doc, 3, "3"); // type
      await flush();
      const save = findButton(r.doc, "保存") as unknown as { disabled?: boolean };
      assertEqual(save.disabled, false, "必填齐全后应可保存");
      clickButton(r.doc, "保存");
      await flush();
      const body = lastBody(r, "PUT");
      assertEqual(body.title, "补测卷", "标题");
      assertEqual(body.subject_id, 2, "科目为非空 id");
      assertEqual(body.grade, 9, "年级为非空 id");
      assertEqual(body.exam_type, 3, "类型为非空 id");
    }],

    // Parent side can reassign the owning kid from the edit dialog.
    ["编辑弹层可修改归属孩子并随保存提交", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      setSelectAt(r.doc, 0, "8"); // 0th Select is owning kid -> 小红
      await flush();
      clickButton(r.doc, "保存");
      await flush();
      assertEqual(lastBody(r, "PUT").kid_id, 8, "归属孩子应切换为新 id");
    }],

    // Esc closes the edit dialog.
    ["Esc 关闭编辑弹层", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑");
      await flush();
      assertIncludes(r.text, "试卷名称", "弹层已打开");
      const win = r.doc.defaultView!;
      win.dispatchEvent(new win.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
      await flush();
      assertNotIncludes(r.text, "试卷名称", "Esc 应关闭弹层");
    }],

    // Esc closes the delete confirmation layer.
    ["Esc 关闭删除确认层", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "删除试卷");
      await flush();
      assertIncludes(r.text, "确认删除", "确认层已打开");
      const win = r.doc.defaultView!;
      win.dispatchEvent(new win.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
      await flush();
      assertNotIncludes(r.text, "确认删除", "Esc 应关闭确认层");
      assertEqual(r.calls.filter((c) => c.method === "DELETE").length, 0, "Esc 关闭不应删除");
    }],

    // --- Page-level operations (delete / add / drag-reorder -> one reorder_pages task) ---
    ["「编辑试卷页」打开编辑器，取消原样回画廊", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑试卷页");
      await flush();
      assertIncludes(r.text, "添加页", "编辑器已打开");
      assertIncludes(r.text, "保存", "编辑器有保存入口");
      const save = findButton(r.doc, "保存") as unknown as { disabled?: boolean };
      assertEqual(save.disabled, true, "无改动时保存禁用");
      clickButton(r.doc, "取消");
      await flush();
      assertNotIncludes(r.text, "添加页", "取消后编辑器关闭");
      assertIncludes(r.text, "第 1 / 2 页", "画廊原样还在");
      assertEqual(
        r.calls.filter((c) => c.url.includes("/pages/ops")).length,
        0,
        "取消不应发请求",
      );
    }],

    ["保存载荷：删页 + 上下移调序 + 新页 → operations 与 file 正确", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑试卷页");
      await flush();
      // Move the first down (page 11) -> order [12, 11]
      clickTitle(r.doc, "下移", 0);
      await flush();
      // Delete the now-second one (still page 11) -> final_order [12, new_0]
      clickTitle(r.doc, "删除这页", 1);
      await flush();
      assertIncludes(r.text, "将删除", "标记删除态");
      addFile(r.doc, "p3.png");
      await flush();
      // The first poll directly gets the settled detail (task completes instantly)
      viewerResponses["/api/exams/5"] = {
        ...EXAM,
        page_count: 2,
        pages: [
          { id: 12, page_index: 1, file_path: "/x/p2.png", sha256: "bb" },
          { id: 99, page_index: 2, file_path: "/x/p3.webp", sha256: "cc" },
        ],
      };
      clickButton(r.doc, "保存");
      await flush();

      const op = [...r.calls].reverse().find((c) => c.url.includes("/pages/ops"));
      assert(op !== undefined, "应发 POST /pages/ops");
      assertEqual(op.method, "POST", "页整理是 POST");
      assert(op.form !== undefined, "multipart 表单");
      const payload = JSON.parse(op.form?.operations ?? "{}") as {
        delete_page_ids: number[];
        final_order: (number | string)[];
      };
      assertEqual(
        JSON.stringify(payload.delete_page_ids),
        JSON.stringify([11]),
        "被删页 id",
      );
      assertEqual(
        JSON.stringify(payload.final_order),
        JSON.stringify([12, "new_0"]),
        "终序：存量页 + 新页占位",
      );
      assertEqual(op.form?.file, "p3.png", "新页文件随表单提交");

      assertNotIncludes(r.text, "添加页", "保存后编辑器关闭");
      assertIncludes(r.text, "页已更新", "首拍轮询即结算");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["任务进行中：乐观重绘 + 处理中提示，轮询完成后换成服务器真相", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑试卷页");
      await flush();
      clickTitle(r.doc, "删除这页", 0); // delete 11 -> final [12]
      await flush();
      // The first poll still gets the old detail (task incomplete) -> stay processing
      clickButton(r.doc, "保存");
      await flush();
      assertIncludes(r.text, "页整理处理中", "入队后显示处理中");
      assertIncludes(r.text, "第 1 / 1 页", "乐观更新：只剩 1 页");
      assertNotIncludes(r.text, "将删除", "编辑器已关闭");
      const redo = findButton(r.doc, "编辑试卷页") as unknown as { disabled?: boolean };
      assertEqual(redo.disabled, true, "任务进行中禁止再次整理");

      // The next poll beat (2s later) gets the settled detail
      viewerResponses["/api/exams/5"] = {
        ...EXAM,
        page_count: 1,
        pages: [{ id: 12, page_index: 1, file_path: "/x/p2.png", sha256: "bb" }],
      };
      await new Promise((resolve) => setTimeout(resolve, 2300));
      assertIncludes(r.text, "页已更新", "轮询完成后提示成功");
      assertNotIncludes(r.text, "处理中", "处理中状态结束");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["最后一页不能被删：提示「至少保留 1 页」并拒绝标记", async () => {
      const r = await mountViewer();
      await flush();
      clickButton(r.doc, "编辑试卷页");
      await flush();
      const markedCount = () =>
        Array.from(r.doc.querySelectorAll("span")).filter((el) => el.textContent === "将删除").length;
      clickTitle(r.doc, "删除这页", 0); // delete 11, leaving 12: allowed
      await flush();
      assertEqual(markedCount(), 1, "第一张已标记");
      // The title is dynamic: after 11 is marked deleted, the only "delete this page" left is 12's
      clickTitle(r.doc, "删除这页", 0); // want to delete 12 too: refused
      await flush();
      assertIncludes(r.text, "至少保留 1 页", "清零守卫上屏");
      assertEqual(markedCount(), 1, "第二张拒绝标记");
      assertEqual(titleCount(r.doc, "取消删除"), 1, "只有第一张可反悔");
      // A mistakenly marked first page can be reverted
      clickTitle(r.doc, "取消删除", 0);
      await flush();
      assertEqual(markedCount(), 0, "取消标记后无删除态");
      assertEqual(titleCount(r.doc, "删除这页"), 2, "两张都恢复未标记");
    }],

    ["保存失败：错误上屏、留在编辑器、不启动轮询", async () => {
      const r = await mountViewer({
        extra: {
          "POST /api/exams/5/pages/ops": { code: 40010, message: "final_order 校验失败" },
        },
      });
      await flush();
      clickButton(r.doc, "编辑试卷页");
      await flush();
      clickTitle(r.doc, "下移", 0);
      await flush();
      clickButton(r.doc, "保存");
      await flush();
      assertIncludes(r.text, "final_order 校验失败", "错误文案上屏");
      assertIncludes(r.text, "添加页", "失败后留在编辑器（选择不丢）");
      const gets = r.requests.filter((u) => u === "/api/exams/5");
      assertEqual(gets.length, 1, "失败后不启动轮询（只有首载详情）");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["无控制台错误", async () => {
      const r = await mountViewer();
      await flush();
      assertEqual(r.errors.length, 0, `控制台错误：${r.errors.join(" | ")}`);
    }],
  ],
};

export default suite;
