/**
 * test/setup.ts -- happy-dom DOM environment.
 *
 * Import order is a hard constraint, not a style issue.
 *
 * vue's runtime-dom reads the global document at module top level:
 *   `const doc = typeof document !== 'undefined' ? document : null`
 * If globalThis.document is still undefined then, vue's internal doc stays null
 * forever and `doc.createElement` later throws `null is not an object` -- unrelated
 * to the mount point or component authoring, and hard to locate.
 *
 * ESM evaluates dependencies in import declaration order, so importing this file
 * (via mount.ts) before vue / business SFCs is enough. mount.ts already places
 * "./setup" first and adds an assertDOMReady() self-check.
 *
 * Why not vitest: see frontend/test/README.md.
 */
import { Window } from "happy-dom";

/** happy-dom window and document (operate these two directly in tests). */
export const testWindow = new Window({ url: "http://localhost/" });
export const testDocument = testWindow.document;

const g = globalThis as Record<string, unknown>;
g.window = testWindow;
g.document = testDocument;

// vue's DOM operations use these constructors (instanceof checks, events, style
// computation, etc). happy-dom's Window has them, but they're not on globalThis,
// so each must be copied over.
for (const key of [
  "Node", "Element", "HTMLElement", "SVGElement", "MathMLElement", "Text", "Comment",
  "DocumentFragment", "Event", "CustomEvent", "MouseEvent", "KeyboardEvent", "NodeList",
  "HTMLCollection", "DOMParser", "getComputedStyle", "requestAnimationFrame",
  "cancelAnimationFrame", "HTMLTemplateElement", "HTMLInputElement", "HTMLSelectElement",
  "HTMLTextAreaElement", "HTMLButtonElement", "HTMLAnchorElement", "HTMLImageElement",
  "CSS", "MutationObserver", "navigator",
  // vue-router reads the bare `history.state` when finalizing navigation (see
  // finalizeNavigation in dist), not only via window.history. Without it, it
  // reports "history is not defined", unrelated to route config correctness.
  "history", "location",
  // The Pinia kid store reads/writes localStorage to persist the selected kid on creation.
  "localStorage", "sessionStorage",
]) {
  if (g[key] === undefined && (testWindow as unknown as Record<string, unknown>)[key] !== undefined) {
    g[key] = (testWindow as unknown as Record<string, unknown>)[key];
  }
}

/**
 * Pre-mount self-check: are DOM globals in place.
 *
 * Guards against a reversed import order: reporting "vue loaded before the DOM
 * environment" is far easier to locate than a mysteriously failing assertion.
 */
export function assertDOMReady(): void {
  if (g.document === undefined || g.SVGElement === undefined) {
    throw new Error(
      "happy-dom DOM environment not in place: ensure this harness (test/mount) is imported before vue and business SFCs.",
    );
  }
}

/**
 * Add a 2D context stub to happy-dom's canvas.
 *
 * happy-dom has no real canvas, so `HTMLCanvasElement.getContext("2d")` returns
 * null; Chart.js then takes its fallback branch and `console.error`s, so every case
 * mounting a chart page (DashboardPanels / ChartWrapper) emits two "Failed to
 * create chart" lines. That's an environment gap, not a business bug.
 *
 * The returned Proxy yields no-op functions for unknown properties, with sensible
 * defaults for special ones (font/lineWidth, canvas returns the element itself).
 * Chart.js only needs the calls not to throw to instantiate; it won't draw pixels.
 */
function installCanvasStub(): void {
  const proto = testWindow.HTMLCanvasElement?.prototype as
    | { getContext?: unknown }
    | undefined;
  if (proto === undefined) return;

  proto.getContext = function (this: unknown) {
    // Chart.js's acquireContext only accepts the context if context.canvas === canvas
    // (chart.cjs: `if (context && context.canvas === canvas)`), so canvas must point
    // back to the calling element here, otherwise everything is null.
    const target: Record<PropertyKey, unknown> = {
      canvas: this,
      font: "10px sans-serif",
      lineWidth: 1,
      lineCap: "butt",
      lineJoin: "miter",
      miterLimit: 10,
      lineDashOffset: 0,
      globalAlpha: 1,
      globalCompositeOperation: "source-over",
      textAlign: "start",
      textBaseline: "alphabetic",
      fillStyle: "#000",
      strokeStyle: "#000",
      shadowBlur: 0,
      shadowColor: "rgba(0,0,0,0)",
      measureText: () => ({ width: 0, actualBoundingBoxAscent: 0, actualBoundingBoxDescent: 0 }),
      getLineDash: () => [],
      createLinearGradient: () => ({ addColorStop: () => {} }),
      createRadialGradient: () => ({ addColorStop: () => {} }),
      createPattern: () => null,
      getImageData: () => ({ data: new Uint8ClampedArray(4), width: 1, height: 1 }),
      putImageData: () => {},
    };
    return new Proxy(target, {
      get(t, key) {
        if (key in t) return t[key];
        // Undeclared properties become no-op methods, so Chart.js calls don't
        // throw on undefined().
        return () => {};
      },
      set(t, key, value) {
        t[key] = value;
        return true;
      },
    });
  } as unknown as typeof proto.getContext;
}
installCanvasStub();

/**
 * happy-dom doesn't implement ResizeObserver, while Chart.js's responsive platform
 * calls `new ResizeObserver(...)` at instantiation. Without it every chart page
 * throws a ReferenceError (caught by "no console errors"). An empty implementation
 * suffices: tests don't verify resize, only that construction doesn't blow up.
 */
if (g.ResizeObserver === undefined) {
  g.ResizeObserver = class {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  };
}

/** Unmount all mounted apps and clear body/storage, so the next case can reuse the same process. */
export function resetDOM(): void {
  testDocument.body.innerHTML = "";
  // The kid store persists "currently selected kid" in localStorage; not clearing
  // it leaks the previous case's selection into the next (the store reads it on
  // creation), making assertions like "set as current kid" start from a wrong initial state.
  testWindow.localStorage.clear();
  testWindow.sessionStorage.clear();
}
