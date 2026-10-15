/**
 * test/overlay-outside-click.test.ts -- dismissing overlays by clicking outside.
 *
 * When Select / DatePicker sits inside a <label>, clicking the blank outside the
 * overlay won't dismiss it (only Esc works), and after picking an option the overlay
 * pops back. The topbar's "switch kid" is fine because it isn't in a <label>.
 *
 * Root cause (reproduced in real Chromium; happy-dom can't reproduce it):
 * a <label> treats a click on any descendant as activation and fires an extra click
 * at the labelled control. That click goes directly to the trigger button, not
 * through the overlay, so `@click.stop` doesn't affect it. Chromium's label
 * forwarding only checks defaultPrevented, so clicks on the overlay and panel must
 * preventDefault.
 *
 * happy-dom doesn't implement label forwarding, so clicking the overlay in tests
 * always looks correct. This file therefore doesn't test "does clicking dismiss"
 * (always true here); it asserts the mechanism the fix relies on: clicks received
 * by the overlay / panel must be defaultPrevented. Reverting to @click.stop turns
 * these cases red.
 */
import { testWindow } from "./setup";
import { defineComponent, h, ref } from "vue";
import type { Document, Element as HappyElement } from "happy-dom";
import { assert, assertEqual, type TestSuite } from "./assert";
import { flush, mountPage } from "./mount";
import Select from "../src/components/ui/Select.vue";
import DatePicker from "../src/components/ui/DatePicker.vue";

const OPTIONS = [
  { value: 1, label: "一年级" },
  { value: 2, label: "二年级" },
];

/**
 * Host: wraps the controls in <label>, mirroring the real pages that hit this bug
 * (ExamCreatePage's subject/grade/type/date, KidFormModal / KidDetailPage's
 * grade/gender).
 */
const Host = defineComponent({
  setup() {
    const grade = ref<number | null>(null);
    const date = ref("");
    return { grade, date, OPTIONS };
  },
  render() {
    return h("div", [
      h(
        "label",
        { class: "flex flex-col gap-1" },
        [
          h("span", "年级"),
          h(Select, {
            modelValue: this.grade,
            "onUpdate:modelValue": (v: number) => {
              this.grade = v;
            },
            options: OPTIONS,
            placeholder: "请选择年级",
          }),
        ],
      ),
      h(
        "label",
        { class: "flex flex-col gap-1" },
        [
          h("span", "考试日期"),
          h(DatePicker, {
            modelValue: this.date,
            "onUpdate:modelValue": (v: string) => {
              this.date = v;
            },
          }),
        ],
      ),
    ]);
  },
});

function find(root: Document | HappyElement, sel: string): HappyElement {
  const el = root.querySelector(sel);
  if (!el) throw new Error(`未找到元素：${sel}`);
  return el;
}

function click(el: unknown, what: string): void {
  if (!el) throw new Error(`未找到可点击元素：${what}`);
  (el as { click: () => void }).click();
}

/**
 * Dispatch a cancelable click bubbling event, returning it for defaultPrevented assertion.
 *
 * Must use dispatchEvent + cancelable:true: Element.click()'s synthetic event isn't
 * cancelable, so defaultPrevented is always false and the mechanism couldn't be
 * asserted. The MouseEvent constructor comes from testWindow, the same window as the
 * mounted document.
 */
function cancelableClick(el: HappyElement): { defaultPrevented: boolean } {
  const ev = new testWindow.MouseEvent("click", { bubbles: true, cancelable: true });
  el.dispatchEvent(ev);
  return ev;
}

/** Whether any ancestor of the node is a <label> -- the precondition of this bug. */
function insideLabel(node: HappyElement): boolean {
  let p: unknown = node.parentElement;
  while (p) {
    if ((p as HappyElement).tagName === "LABEL") return true;
    p = (p as HappyElement).parentElement;
  }
  return false;
}

const suite: TestSuite = {
  name: "浮层外点击关闭",
  cases: [
    ["宿主复刻报障结构：两个控件都在 <label> 内", async () => {
      const r = await mountPage(Host, { router: false });
      assertEqual(r.doc.querySelectorAll("label").length, 2, "label 数量");
      const selectRoot = find(r.doc, "[data-select-root]");
      const dateRoot = find(r.doc, "[data-datepicker-root]");
      assert(insideLabel(selectRoot), "Select 的祖先链上应存在 <label>");
      assert(insideLabel(dateRoot), "DatePicker 的祖先链上应存在 <label>");
    }],

    ["Select 点空白遮罩：click 必须 preventDefault", async () => {
      const r = await mountPage(Host, { router: false });
      click(find(r.doc, '[role="combobox"]'), "Select 触发器");
      await flush(); // the overlay is v-if, needs a frame to render
      const ev = cancelableClick(find(find(r.doc, "[data-select-root]"), "div.fixed"));
      assertEqual(ev.defaultPrevented, true, "遮罩 click 未 preventDefault → label 补发 click 会重新打开");
    }],

    ["Select 点面板（选中选项）：click 必须 preventDefault", async () => {
      const r = await mountPage(Host, { router: false });
      click(find(r.doc, '[role="combobox"]'), "Select 触发器");
      const ev = cancelableClick(find(r.doc, '[role="listbox"]'));
      assertEqual(ev.defaultPrevented, true, "面板 click 未 preventDefault → 选中后浮层会立刻弹回");
    }],

    ["DatePicker 点月历面板：click 必须 preventDefault", async () => {
      const r = await mountPage(Host, { router: false });
      const root = find(r.doc, "[data-datepicker-root]");
      click(find(root, "button"), "DatePicker 触发器");
      const ev = cancelableClick(find(r.doc, '[role="dialog"]'));
      assertEqual(ev.defaultPrevented, true, "DatePicker 面板 click 未 preventDefault");
    }],

    ["DatePicker 点空白遮罩：click 必须 preventDefault", async () => {
      const r = await mountPage(Host, { router: false });
      const root = find(r.doc, "[data-datepicker-root]");
      click(find(root, "button"), "DatePicker 触发器");
      await flush(); // the overlay is v-if, needs a frame to render
      const ev = cancelableClick(find(root, "div.fixed"));
      assertEqual(ev.defaultPrevented, true, "DatePicker 遮罩 click 未 preventDefault");
    }],
  ],
};

export default suite;
