/**
 * test/button-unified.test.ts -- unified shadcn Button (components/ui/Button.vue).
 *
 * Pins the unified Button's behaviors:
 *   - `class` prop merged LAST via cn() (caller overrides like size-11 win);
 *   - clicks suppressed while loading/disabled;
 *   - variants render their backing classes.
 */
import "./setup";
import { defineComponent, h } from "vue";
import { assert, assertEqual, type TestSuite } from "./assert";
import { flush, mountPage } from "./mount";
import Button from "../src/components/ui/Button.vue";

let clicked = 0;
const Host = defineComponent({
  setup() {
    return {};
  },
  render() {
    return h("div", [
      h(Button, { variant: "outline", class: "size-11 sm:size-9", onClick: () => { clicked++; } }, () => "override"),
      h(Button, { variant: "destructive", loading: true, onClick: () => { clicked++; } }, () => "busy"),
      h(Button, { variant: "ghost", disabled: true, onClick: () => { clicked++; } }, () => "off"),
    ]);
  },
});

async function buttons(): Promise<HTMLButtonElement[]> {
  const r = await mountPage(Host, { router: false });
  await flush();
  return Array.from(r.doc.querySelectorAll("button")) as unknown as HTMLButtonElement[];
}

function click(el: HTMLButtonElement): void {
  el.dispatchEvent(new el.ownerDocument.defaultView!.Event("click", { bubbles: true }));
}

const suite: TestSuite = {
  name: "Button 统一（shadcn 版吸收旧版行为）",
  cases: [
    ["class 覆盖在变体之后生效（size-11 覆盖 size 默认）", async () => {
      const [btn] = await buttons();
      const cls = btn!.className;
      assert(/\bsize-11\b/.test(cls), `覆盖类应保留 size-11，实际：${cls}`);
      assert(/\bbg-control\b/.test(cls), `outline 变体应为 bg-control，实际：${cls}`);
    }],

    ["loading 时点击被吞掉", async () => {
      const [, busy] = await buttons();
      clicked = 0;
      click(busy!);
      await flush();
      assertEqual(clicked, 0, "loading 按钮不应触发 click");
    }],

    ["loading 旋转器渲染图标字形而不是字面文字", async () => {
      const [, busy] = await buttons();
      const raw = busy!.textContent ?? "";
      assert(!/refresh/i.test(raw), `不应闪现字面 "Refresh"：${raw}`);
      assert(raw.includes("\uE5D5"), `应含 refresh 的 PUA 字形 E5D5，实际：${raw.split("").map(c => c.charCodeAt(0).toString(16)).join(" ")}`);
    }],

    ["disabled 时点击被吞掉", async () => {
      const [, , off] = await buttons();
      clicked = 0;
      click(off!);
      await flush();
      assertEqual(clicked, 0, "disabled 按钮不应触发 click");
    }],

    ["正常按钮点击透出", async () => {
      const [btn] = await buttons();
      clicked = 0;
      click(btn!);
      await flush();
      assertEqual(clicked, 1, "正常按钮应触发一次 click");
    }],
  ],
};

export default suite;
