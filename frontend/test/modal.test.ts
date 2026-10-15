/**
 * test/modal.test.ts -- the shared Modal shell's layout contract.
 *
 * Long forms (e.g. the kid form's subject grid) used to overflow the panel and push
 * the action buttons out of the 90vh viewport, forcing a second scroll to reach them.
 * The fix makes the panel a flex column: the title and footer are pinned, and only
 * the body scrolls. These cases assert that structure directly (the property that
 * keeps the buttons reachable) rather than any pixel height, which happy-dom can't
 * observe.
 */
import { mountPage } from "./mount";
import { defineComponent, h } from "vue";
import { assert, assertEqual, type TestSuite } from "./assert";
import Modal from "../src/components/Modal.vue";

/** Host renders the Modal open with a body and a footer button. */
const Host = defineComponent({
  render() {
    return h(
      Modal,
      { show: true, title: "标题" },
      {
        default: () => h("div", { class: "space-y-4" }, "正文"),
        footer: () => h("button", { type: "button" }, "确定"),
      },
    );
  },
});

/** Same, but with flexBody so the body is a flex column for the slot to fill. */
const FlexHost = defineComponent({
  render() {
    return h(
      Modal,
      { show: true, title: "标题", flexBody: true },
      {
        default: () => h("div", { class: "flex min-h-0 flex-1 flex-col" }, "正文"),
        footer: () => h("button", { type: "button" }, "确定"),
      },
    );
  },
});

function dialog(r: Awaited<ReturnType<typeof mountPage>>) {
  const panel = r.doc.querySelector('[role="dialog"]');
  assert(panel, "弹窗应渲染");
  return panel!;
}

const suite: TestSuite = {
  name: "Modal（标题/底栏固定，正文滚动）",
  cases: [
    ["底栏与正文是同级节点，底栏不在滚动区内", async () => {
      const r = await mountPage(Host);
      const panel = dialog(r);
      const body = panel.querySelector('[data-testid="modal-body"]');
      const footer = panel.querySelector('[data-testid="modal-footer"]');
      assert(body, "应有正文容器");
      assert(footer, "应有底栏容器");
      assertEqual(body!.parentElement, panel, "正文应是面板的直接子级");
      assertEqual(footer!.parentElement, panel, "底栏应是面板的直接子级（固定区）");
      assert(!body!.contains(footer!), "底栏不应被包进滚动正文里");
    }],

    ["正文是滚动容器，面板本身裁剪溢出", async () => {
      const r = await mountPage(Host);
      const panel = dialog(r);
      const body = panel.querySelector('[data-testid="modal-body"]');
      assert(body!.className.includes("overflow-y-auto"), "正文应可纵向滚动");
      assert(body!.className.includes("flex-1"), "正文应撑满剩余高度");
      assert(panel.className.includes("overflow-hidden"), "面板应裁剪溢出");
      assert(!panel.className.includes("overflow-y-auto"), "面板本身不应滚动");
      assert(panel.className.includes("flex-col"), "面板应是纵向 flex 布局");
    }],

    ["滚动正文留出内边距，避免聚焦环被裁掉", async () => {
      // The body is an overflow container; without inner padding it clips the 2px
      // focus ring of flush controls. Negative margin of the same size keeps the
      // content aligned while still giving the ring room.
      const r = await mountPage(Host);
      const body = dialog(r).querySelector('[data-testid="modal-body"]');
      assert(body!.className.includes("px-1"), "正文应有横向内边距容纳聚焦环");
      assert(body!.className.includes("-mx-1"), "正文应用等量负外边距保持对齐");
    }],

    ["标题在正文之外，不随正文滚动", async () => {
      const r = await mountPage(Host);
      const panel = dialog(r);
      const body = panel.querySelector('[data-testid="modal-body"]');
      const title = panel.querySelector("h2");
      assert(title, "标题应渲染");
      assert(!body!.contains(title!), "标题不应在滚动区内");
    }],

    ["底栏渲染传入的按钮", async () => {
      const r = await mountPage(Host);
      const panel = dialog(r);
      const footer = panel.querySelector('[data-testid="modal-footer"]');
      assert((footer!.textContent ?? "").includes("确定"), "底栏应渲染按钮文案");
    }],

    ["flexBody：正文成为纵向 flex 容器，供插槽自行填充", async () => {
      const r = await mountPage(FlexHost);
      const body = dialog(r).querySelector('[data-testid="modal-body"]');
      assert(body!.className.includes("flex"), "flexBody 正文应为 flex 容器");
      assert(body!.className.includes("flex-col"), "flexBody 正文应纵向排列");
      assert(body!.className.includes("overflow-y-auto"), "flexBody 正文仍可兜底滚动");
    }],
  ],
};

export default suite;
