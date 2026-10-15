/**
 * test/mount.ts -- mount a single-page component and assert the rendered result.
 *
 * Usage (order matters, see the notes in setup.ts):
 *   import { mountPage } from "../test/mount";   // (1) must come first
 *   import ExamListPage from "../src/pages/ExamListPage.vue";  // (2) SFC after
 *
 *   const r = await mountPage(ExamListPage, {
 *     responses: { "/api/exams": { items: [], total: 0 } },
 *   });
 *   assert(r.text.includes("暂无试卷"));
 */
import { assertDOMReady, resetDOM, testDocument, testWindow } from "./setup";
import { createApp, type Component } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import type { Document } from "happy-dom";

// "./setup" must be this file's first import -- it installs document onto
// globalThis before the vue module body executes. Always read setup.ts before
// changing this file's import order.

/** Mount result: text content, DOM handle, requests made, captured errors. */
export interface MountResult {
  /**
   * Whole-page textContent. It's a getter, read lazily: after an interaction
   * re-renders, `r.text` already yields the latest copy.
   */
  readonly text: string;
  /**
   * Document handle, using happy-dom's own Document type.
   *
   * happy-dom's Document is a different type system from lib.dom (missing dozens
   * of members), so assigning between them only produces errors; the querySelector
   * / textContent used in assertions have matching signatures.
   */
  doc: Document;
  /** URLs actually requested (in order), for asserting "requested once / which endpoint". */
  requests: string[];
  /**
   * Full calls, one-to-one with requests: method, JSON body (string), and multipart
   * form fields. For asserting save/upload payloads.
   */
  calls: Array<{ method: string; url: string; body: string | null; form?: Record<string, string> }>;
  /** Content captured from console.error and the vue errorHandler. */
  errors: string[];
  /** Navigation paths recorded by the test router (in order), for asserting router.push targets. */
  navigations: string[];
}

export interface MountOptions {
  /**
   * Path substring -> response body.
   *
   * If the value has a `code` field it is returned as-is (can simulate the error
   * envelope {code,message}); otherwise it is wrapped as `{code:0,data:value}`.
   * Matched by substring, first hit wins.
   */
  responses?: Record<string, unknown>;
  /** Default response body when no path matches; defaults to `{code:0,data:null}`. */
  fallback?: unknown;
  /**
   * Whether to install the test router, default true.
   *
   * Without it, useRouter() in pages gets no instance: Vue logs an
   * `injection "Symbol(router)" not found` warning per component and navigation
   * clicks throw. Uses in-memory history + a catch-all route, loads no real pages,
   * and only records navigations.
   */
  router?: boolean;
  /**
   * Initial route path, default "/". Detail pages use `useRoute().params.id` for
   * the primary key, so pass e.g. `/exams/5`; the harness has parameterized routes
   * matching the app's route table (papers/kids/exams :id), else params won't resolve.
   */
  path?: string;
  /**
   * localStorage entries (key -> value) preset before mount.
   *
   * Must be set here, not in a case: mountPage's opening resetDOM() clears
   * localStorage, wiping a case's setItem too. Anything a component reads from
   * persistence during setup (kid token, kid store selectedId, theme) belongs here.
   */
  storage?: Record<string, string>;
}

/** Parameterized routes matching src/router (only for resolving params, no real page render). */
const TEST_ROUTE_PATTERNS = [
  "/",
  "/welcome",
  "/login",
  "/exams",
  "/exams/upload",
  "/exams/:id",
  { path: "/kids", name: "kids" },
  "/kids/:id",
  "/exams",
  "/exams/new",
  "/exams/:id",
  "/mastery",
  "/review",
  "/storage",
  "/image",
  "/kids-auth",
  "/parents",
  "/tasks",
  "/subjects",
  "/knowledge-points",
  "/system",
  // Kid side: routes are clean /kid/*, session credentials (paperly.kid_token)
  // do not appear in sub-paths (src/lib/kidSession.ts). Order matches src/router.
  // Kid routes carry `meta.audience="kid"` so pages can distinguish parent/kid
  // behavior (synced with src/router).
  { path: "/kid" },
  { path: "/kid/exams", meta: { audience: "kid" } },
  { path: "/kid/exams/upload", meta: { audience: "kid" } },
  { path: "/kid/exams/:id", meta: { audience: "kid" } },
  { path: "/kid/subjects", meta: { audience: "kid" } },
  { path: "/kid/knowledge-points", meta: { audience: "kid" } },
  // Standalone entry page (no nav shell), matching src/router.
  "/invite/:code",
  { path: "/kid/login" },
  // Legacy entry `/kid/:accessCode`: the real route is a redirect (to /invite/:code);
  // a matching record is kept here only as a placeholder to avoid the catch-all.
  "/kid/:accessCode",
  "/kid/:accessCode/:rest(.*)*",
];

/** Stub for byte-stream endpoints (e.g. page images): fetchBlob calls
 *  response.blob(), so return minimal PNG bytes. */
async function fakeBlob(): Promise<Blob> {
  return new Blob([new Uint8Array([0x89, 0x50, 0x4e, 0x47])]);
}

/** Install the fetch stub, recording each call's URL/method/body. */
function stubFetch(
  responses: Record<string, unknown>,
  fallback: unknown,
  requests: string[],
  calls: Array<{ method: string; url: string; body: string | null; form?: Record<string, string> }>,
): void {
  (globalThis as Record<string, unknown>).fetch = async (
    input: unknown,
    init?: { method?: string; body?: unknown },
  ) => {
    const url = String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    // The client passes a stringified string for JSON bodies; FormData for multipart.
    const raw = init?.body;
    const reqBody = typeof raw === "string" ? raw : null;
    let form: Record<string, string> | undefined;
    if (raw instanceof FormData) {
      form = {};
      for (const [k, v] of raw.entries()) {
        // Bun types FormData.entries() as [string, string], so the non-string
        // branch narrows to never; at runtime it is a File, hence the assertion.
        form[k] = typeof v === "string" ? v : (v as File).name;
      }
    }
    requests.push(url);
    calls.push({ method, url, body: reqBody, form });
    for (const [key, responseBody] of Object.entries(responses)) {
      // The key may be written as "POST /api/kids" to constrain the method (when
      // one path has different GET/POST responses); a bare path substring key
      // (method-agnostic) is also supported.
      const typed = /^(GET|POST|PUT|PATCH|DELETE)\s+(\S.*)$/.exec(key);
      if (typed) {
        if (method !== typed[1] || !url.includes(typed[2] ?? "")) continue;
      } else if (!url.includes(key)) {
        continue;
      }
      const payload =
        responseBody && typeof responseBody === "object" && "code" in responseBody
          ? responseBody
          : { code: 0, data: responseBody };
      return { ok: true, status: 200, _data: payload, json: async () => payload, blob: fakeBlob };
    }
    return { ok: true, status: 200, _data: fallback, json: async () => fallback, blob: fakeBlob };
  };
}

/**
 * flush runs two beats "microtask -> one macrotask", letting onMounted's await
 * and DOM updates land. Called by mountPage and by cases after an interaction
 * (click/input) to wait for the component to re-fetch and render.
 */
export async function flush(): Promise<void> {
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 50));
}

/**
 * mountPage mounts a component, waits for onMounted's async tail, and returns an assertion handle.
 *
 * The mount point must be appended into body: without append the render happens but
 * no node is in document.body, so text and querySelector are empty and it looks like
 * the component didn't render.
 */
export async function mountPage(
  component: Component,
  options: MountOptions = {},
): Promise<MountResult> {
  assertDOMReady();
  resetDOM();

  const requests: string[] = [];
  const errors: string[] = [];
  const navigations: string[] = [];
  const calls: Array<{ method: string; url: string; body: string | null; form?: Record<string, string> }> = [];
  stubFetch(options.responses ?? {}, options.fallback ?? { code: 0, data: null }, requests, calls);

  const app = createApp(component);
  // pinia is a global hook that useXxxStore in pages depends on; without it,
  // it throws "getActivePinia". Installing it is harmless to other pages.
  app.use(createPinia());
  app.config.errorHandler = (err) => errors.push(String(err));
  const originalError = console.error;
  console.error = (...args: unknown[]) => {
    errors.push(args.map(String).join(" "));
    originalError(...args);
  };

  if (options.router !== false) {
    const Empty = { render: () => null };
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        ...TEST_ROUTE_PATTERNS.map((pattern) =>
          typeof pattern === "string" ? { path: pattern, component: Empty } : { ...pattern, component: Empty },
        ),
        { path: "/:pathMatch(.*)*", component: Empty },
      ],
    });
    // Record fullPath (with query) so jumps like /exams?kid_id=7 can be asserted.
    router.afterEach((to) => navigations.push(to.fullPath));
    app.use(router);
    // Run the initial navigation then clear the record, so a case doesn't see a
    // path it didn't trigger.
    await router.push(options.path ?? "/");
    navigations.length = 0;
  }

  // A component may read localStorage during setup (kid token, theme preference...),
  // so this must be written before mount; writing it in a case gets wiped by resetDOM above.
  for (const [k, v] of Object.entries(options.storage ?? {})) {
    testWindow.localStorage.setItem(k, v);
  }

  const container = testDocument.createElement("div");
  testDocument.body.appendChild(container);
  // vue's mount signature is typed against lib.dom's Element, structurally
  // different from happy-dom's Element. The difference is not behavioral, so it's
  // closed off at this one point instead of casting everywhere.
  app.mount(container as unknown as Parameters<typeof app.mount>[0]);

  // Two beats: microtask completes onMounted's await, then one more beat for DOM updates to land.
  await flush();

  console.error = originalError;
  return {
    // getter: re-renders after interaction; read lazily to avoid stale copy from mount time.
    get text() {
      return testDocument.body.textContent ?? "";
    },
    doc: testDocument,
    requests,
    calls,
    errors,
    navigations,
  };
}
