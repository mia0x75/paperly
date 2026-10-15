import { nextTick } from "vue";
import { testWindow } from "./setup";
import type { Document, Element as HappyElement } from "happy-dom";

/**
 * test/controls.ts -- test helpers for driving ui/Select and ui/DatePicker.
 *
 * These are custom controls, not native <select> / <input type=date>, so tests
 * can't set .value directly: Select options are menu items you click, DatePicker
 * needs a click on a calendar cell. Funneling interaction here keeps page tests
 * expressing "which value was selected" instead of DOM structure.
 *
 * Both controls' overlays are always mounted (shown/hidden via hidden, see the
 * component comments), so options can be found right after opening the trigger
 * without waiting a microtask; lookups are scoped by control root so same-value
 * options across multiple controls on a page don't interfere.
 */

function click(el: unknown, what: string): void {
  if (!el) throw new Error(`clickable element not found: ${what}`);
  (el as { click: () => void }).click();
}

/** All Select triggers on the page (in DOM order, i.e. page appearance order). */
export function selectTriggers(doc: Document): HappyElement[] {
  return Array.from(doc.querySelectorAll('[role="combobox"]'));
}

function selectRoot(doc: Document, index: number): HappyElement {
  const t = selectTriggers(doc)[index];
  if (!t) throw new Error(`Select trigger #${index} not found`);
  return (t.closest("[data-select-root]") ?? doc.body) as HappyElement;
}

/** Text currently shown by the index-th Select (placeholder when unselected). */
export function selectText(doc: Document, index: number): string {
  const t = selectTriggers(doc)[index];
  return t ? (t.textContent ?? "").trim() : "";
}

/** Select the option of the index-th Select whose data-value equals value. */
export function setSelectAt(doc: Document, index: number, value: string): void {
  const root = selectRoot(doc, index);
  click(selectTriggers(doc)[index], `Select trigger #${index}`);
  click(root.querySelector(`[role="option"][data-value="${value}"]`), `option ${value} of Select #${index}`);
}

/** Select the index-th Select's item by visible text (for labels with spaces/Chinese). */
export function selectByText(doc: Document, index: number, label: string): void {
  const root = selectRoot(doc, index);
  click(selectTriggers(doc)[index], `Select trigger #${index}`);
  const opt = Array.from(root.querySelectorAll('[role="option"]')).find((o) =>
    (o.textContent ?? "").includes(label),
  );
  click(opt, `option "${label}" of Select #${index}`);
}

/**
 * Pick the given date (ISO, YYYY-MM-DD) on the DatePicker's month calendar.
 *
 * The calendar defaults to the current month (or the selected value's month);
 * when the target month differs, page via the arrows first, then click the date
 * cell with data-date.
 */
export async function pickDate(doc: Document, iso: string): Promise<void> {
  const trigger = doc.querySelector('[aria-haspopup="dialog"]');
  click(trigger, "DatePicker trigger");
  const root = trigger?.closest("[data-datepicker-root]");
  const calendar = root?.querySelector("[data-placeholder]");
  if (!root || !calendar) throw new Error("month calendar not mounted");
  // Compute the months to page at once: placeholder is reactive, and the DOM does
  // not update between synchronous clicks, so reading it repeatedly yields the
  // stale value. Click the diff count, then await one tick so the calendar renders
  // to the target month.
  const [sy = 0, sm = 0] = (calendar.getAttribute("data-placeholder") ?? "").split("-").map(Number);
  const [ty = 0, tm = 0] = iso.split("-").map(Number);
  const diff = (ty - sy) * 12 + (tm - sm);
  const arrow = diff > 0 ? '[aria-label="下个月"]' : '[aria-label="上个月"]';
  for (let i = 0; i < Math.abs(diff); i++) click(root.querySelector(arrow), "page month");
  if (diff !== 0) await nextTick();
  click(root.querySelector(`[data-date="${iso}"]`), `date cell ${iso}`);
}

/** Fire the native input's input event (for text/password fields still kept native). */
export function setNativeInput(el: unknown, value: string): void {
  const input = el as { value: string; dispatchEvent: (e: unknown) => void };
  input.value = value;
  input.dispatchEvent(new testWindow.Event("input", { bubbles: true }));
}

/**
 * Find a button by visible text, throwing if not found (so the error points at
 * the missing button, not a downstream click).
 *
 * Uses includes rather than equality: Material Symbols icons are font ligatures,
 * and Icon.vue renders literal text (e.g. "arrow_forward更多"); equality would miss
 * icon buttons.
 */
export function findButton(doc: Document, label: string): unknown {
  const el = Array.from(doc.querySelectorAll("button")).find((b) =>
    (b.textContent ?? "").includes(label),
  );
  if (!el) throw new Error(`button not found: ${label}`);
  return el;
}

export function clickButton(doc: Document, label: string): void {
  click(findButton(doc, label), `button "${label}"`);
}
