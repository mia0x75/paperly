/**
 * test/parent-guard.test.ts -- parent-side login guard (router.parentGuard).
 *
 * Like onboarding-routes.test, builds an in-memory router from the real `routes` +
 * `parentGuard`, pinning four rules:
 *   1. no token -> parent-shell routes (audience=parent) redirect to /login with a
 *      redirect param back;
 *   2. /kid/*, /welcome and /login always pass (credential / entry pages);
 *   3. with a token -> allowed to the target route;
 *   4. unshelled routes (catch-all etc.) aren't intercepted.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { parentGuard, routes } from "../src/router/index";
import { createMemoryHistory, createRouter, type RouteRecordRaw } from "vue-router";

/**
 * Build an in-memory router from the real route table + guard (lazy pages are
 * replaced by an empty component).
 */
function guardedRouter(): ReturnType<typeof createRouter> {
  const Empty = { render: () => null };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: (routes as RouteRecordRaw[]).map(
      (r) =>
        ({
          ...r,
          component: Empty,
          children: (r.children ?? []).map((c) => ({ ...c, component: Empty })),
        }) as RouteRecordRaw,
    ),
  });
  router.beforeEach(parentGuard);
  return router;
}

const suite: TestSuite = {
  name: "parentGuard（家长登录拦截）",
  cases: [
    ["无 token：家长壳子页被重定向到 /login 并带 redirect", async () => {
      const router = guardedRouter();
      await router.push("/storage");
      assertEqual(router.currentRoute.value.path, "/login", "应落在登录页");
      assertEqual(router.currentRoute.value.query.redirect, "/storage", "回跳参数应为原页");
    }],

    ["无 token：访问首页同样被拦", async () => {
      const router = guardedRouter();
      await router.push("/");
      assertEqual(router.currentRoute.value.path, "/login", "首页属家长壳应被拦");
      assertEqual(router.currentRoute.value.query.redirect, "/", "回跳首页");
    }],

    ["无 token：带查询串的子页回跳保留查询", async () => {
      const router = guardedRouter();
      await router.push("/exams?kid_id=7");
      assertEqual(router.currentRoute.value.query.redirect, "/exams?kid_id=7", "回跳保留原样");
    }],

    ["无 token：/kid/* 子树放行（孩子端凭 accessCode）", async () => {
      const router = guardedRouter();
      await router.push("/kid/subjects");
      assertEqual(router.currentRoute.value.path, "/kid/subjects", "孩子端不拦");
    }],

    ["无 token：/login 与 /welcome 放行（防握手循环）", async () => {
      const router = guardedRouter();
      await router.push("/login");
      assertEqual(router.currentRoute.value.path, "/login", "登录页原地停留");
      await router.push("/welcome");
      assertEqual(router.currentRoute.value.path, "/welcome", "欢迎页放行");
    }],

    ["有 token：家长壳子页放行", async () => {
      // The guard reads localStorage synchronously, so write it before building the router.
      const storage = globalThis as unknown as { localStorage: Storage };
      storage.localStorage.setItem("paperly.parent_token", "jwt-abc");
      try {
        const router = guardedRouter();
        await router.push("/storage");
        assertEqual(router.currentRoute.value.path, "/storage", "带 token 应放行");
      } finally {
        storage.localStorage.removeItem("paperly.parent_token");
      }
    }],

    ["未知路径 catch-all 不直拦（落回家长壳链）", async () => {
      const router = guardedRouter();
      await router.push("/no-such-path");
      // catch-all redirects to "/", then parentGuard intercepts it to /login.
      assertEqual(router.currentRoute.value.path, "/login", "catch-all 落回家长壳链路上");
    }],
  ],
};

export default suite;