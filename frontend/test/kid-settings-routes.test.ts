/**
 * test/kid-settings-routes.test.ts -- kid-side subject / knowledge-point routes.
 *
 * Kid routes hang under `/kid`; the literals /kid, /kid/exams, /kid/subjects ...
 * never carry :accessCode -- the credential is the paperly.kid_token session and
 * stays out of the URL. Code-bearing links `/kid/:code*` redirect to /invite/:code.
 *
 * Two things tend to break quietly:
 *   1. "/kid/subjects" must not be swallowed by `/kid/:accessCode`: if the new
 *      route were mounted there, the literal path would miss and quietly land on
 *      kid-home. The real route table resolve pins the ownership.
 *   2. It must be the same page component as the parent side. The assertion is on
 *      route.component reference equality (both routes write the same
 *      `() => import(...)`), not rendered output -- `===` proves identity, not
 *      mere similarity.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { routes } from "../src/router/index";
import { createMemoryHistory, createRouter, type RouteRecordRaw } from "vue-router";
import { KID_TOKEN_KEY } from "../src/lib/kidSession";
import { testWindow } from "./setup";

/** Build an in-memory router from the real route table: resolve does not trigger lazy loading. */
function realRouter() {
  return createRouter({ history: createMemoryHistory(), routes: routes as RouteRecordRaw[] });
}

interface Resolved {
  name: string;
  path: string;
  params: Record<string, unknown>;
}

/**
 * Resolve and normalize name: Vue Router's name type includes null, and
 * String(null) yields "null".
 */
function resolve(path: string): Resolved {
  const to = realRouter().resolve(path);
  return { name: String(to.name), path: to.path, params: to.params };
}

/** Find the record's component reference by name in the route table (the lazy-load function). */
function componentOf(name: string): unknown {
  for (const r of routes) {
    for (const c of r.children ?? []) {
      if (c.name === name) return c.component;
    }
    if (r.name === name) return r.component;
  }
  return undefined;
}

/** Actually load a route's lazy component and return its default export. */
async function load(name: string): Promise<unknown> {
  const loader = componentOf(name) as (() => Promise<{ default?: unknown }>) | undefined;
  if (!loader) return undefined;
  const mod = await loader();
  return mod.default ?? mod;
}

const suite: TestSuite = {
  name: "孩子端设置路由（/kid/subjects · /kid/knowledge-points）",
  cases: [
    ["/kid/subjects 解析到 kid-subjects，未被 /kid/:accessCode 吞掉", async () => {
      const to = resolve("/kid/subjects");
      assertEqual(to.name, "kid-subjects", "应命中静态段 kid-subjects");
      assertEqual(to.path, "/kid/subjects", "路径保持字面量，不带 :accessCode");
      // Must not be eaten by the dynamic segment: params must not contain accessCode.
      assertEqual(
        Object.keys(to.params).join(","),
        "",
        "不应带出 accessCode 参数（否则说明落到了 /kid/:accessCode）",
      );
    }],

    ["/kid/knowledge-points 解析到 kid-knowledge-points", async () => {
      const to = resolve("/kid/knowledge-points");
      assertEqual(to.name, "kid-knowledge-points", "应命中静态段");
      assertEqual(to.path, "/kid/knowledge-points", "路径保持字面量");
      assertEqual(Object.keys(to.params).join(","), "", "不应带出 accessCode 参数");
    }],

    ["两条路由渲染的组件与家长端是同一份（同一模块实例）", async () => {
      // The route table holds two independent arrow functions whose references
      // aren't equal anyway (ESM returns a fresh function per import()), so
      // comparing references would only prove "both wrote import". Both loaders are
      // loaded and their module defaults compared: the same module imported twice
      // yields the same component object via the ESM cache.
      const [kidSub, parentSub, kidKp, parentKp] = await Promise.all([
        load("kid-subjects"),
        load("subjects"),
        load("kid-knowledge-points"),
        load("knowledge-points"),
      ]);
      assert(kidSub !== undefined, "kid-subjects 的 loader 应能加载出组件");
      assert(
        kidSub === parentSub,
        "孩子端科目页必须复用家长端同一份组件（不是复制一份）",
      );
      assert(kidKp === parentKp, "孩子端知识点页必须复用家长端同一份组件");
    }],

    ["家长端 /subjects、/knowledge-points 未被顶掉", async () => {
      assertEqual(resolve("/subjects").name, "subjects", "家长端科目路由仍可达");
      assertEqual(
        resolve("/knowledge-points").name,
        "knowledge-points",
        "家长端知识点路由仍可达",
      );
    }],

    ["入口 /kid/:accessCode 解析到 kid-entry（重定向到邀请页）", async () => {
      assertEqual(
        resolve("/kid/abc123").name,
        "kid-entry",
        "/kid/:accessCode 应是入口，落到 kid-entry 才会 redirect 到 /invite/:code",
      );
      // Also pins that it does not render KidLayout: the entry is only a redirect,
      // and hanging a layout would first draw a frame of blank top bar.
      assertEqual(componentOf("kid-entry"), undefined, "kid-entry 不应挂组件");
    }],

    ["孩子端子页面是字面量 /kid/*，不带 accessCode", async () => {
      assertEqual(resolve("/kid").name, "kid-home", "/kid 即首页");
      assertEqual(resolve("/kid/exams").name, "kid-exams", "试卷是字面量路由");
      assertEqual(resolve("/kid/exams/9").name, "kid-exam-viewer", "卷面路由同理");
      // /kid/exams' name must be neither kid-entry nor carry a code -- the kid top
      // bar's papers entry always points to /kid/exams.
      assertEqual(
        resolve("/kid/exams").path,
        "/kid/exams",
        "试卷路由路径应是干净的 /kid/exams（不被 :accessCode 吞掉）",
      );
    }],

    ["/kid 布局下的全部子路由都带 meta.audience=\"kid\"", async () => {
      const kidLayout = routes.find((r) => r.path === "/kid");
      assert(kidLayout !== undefined, "应存在 /kid 布局路由");
      const children = kidLayout!.children ?? [];
      assertEqual(children.length, 7, `孩子端应有 7 条子路由，实际 ${children.length}`);
      for (const c of children) {
        assertEqual(
          c.meta?.audience,
          "kid",
          `路由 ${String(c.name)} 应带 meta.audience=kid`,
        );
      }
    }],

    ["旧的带码深链 /kid/abc123/exams 解析到 kid-entry-deep（剥码转发）", async () => {
      assertEqual(
        resolve("/kid/abc123/exams").name,
        "kid-entry-deep",
        "旧深链应走兼容分支，收码后转发到 /kid/exams",
      );
      assertEqual(
        resolve("/kid/abc123/subjects").name,
        "kid-entry-deep",
        "设置旧深链同理",
      );
    }],

    ["push 入口 /kid/abc123 重定向到邀请页，且不写任何会话存储", async () => {
      testWindow.localStorage.clear();
      const router = realRouter();
      await router.push("/kid/abc123");
      assertEqual(router.currentRoute.value.path, "/invite/abc123", "入口应并到邀请页");
      assertEqual(
        testWindow.localStorage.getItem(KID_TOKEN_KEY),
        null,
        "入口只做 redirect，不写会话 token（token 由邀请页/登录页写入）",
      );
    }],

    ["push 旧深链 /kid/abc123/exams 同样并到邀请页", async () => {
      testWindow.localStorage.clear();
      const router = realRouter();
      await router.push("/kid/abc123/exams");
      assertEqual(
        router.currentRoute.value.path,
        "/invite/abc123",
        "旧深链统一引导到设 PIN 流程",
      );
    }],

    ["两条路由名与既有路由不重名", async () => {
      const names: string[] = [];
      for (const r of routes) {
        for (const c of r.children ?? []) {
          if (c.name) names.push(String(c.name));
        }
        if (r.name) names.push(String(r.name));
      }
      const dup = names.filter((n, i) => names.indexOf(n) !== i);
      assertEqual(dup.length, 0, `不应有重名路由：${dup.join(", ")}`);
    }],

    ["/kid/subjects 不会被 catch-all 吞掉", async () => {
      const to = resolve("/kid/subjects");
      assert(
        !to.name.includes("pathMatch"),
        "不应落到 catch-all（说明路由表里路径写错了）",
      );
    }],
  ],
};

export default suite;