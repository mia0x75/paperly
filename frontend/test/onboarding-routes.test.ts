/**
 * test/onboarding-routes.test.ts -- first-run onboarding guard (router.onboardingGuard).
 *
 * Page-level tests' mountStep uses its own in-memory router without real guards; this file
 * builds a real guard router directly from `routes` + `onboardingGuard`, pinning three rules:
 *   1. Onboarding incomplete -> any non-/welcome path redirects to /welcome;
 *   2. Completed -> allowed to the target route;
 *   3. Direct visits to /welcome are never redirected (prevents a handshake loop).
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { resetOnboardingCache } from "../src/lib/onboarding";
import { onboardingGuard, routes } from "../src/router/index";
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
  // Attach the guard explicitly: the real entry is router/index.ts's beforeEach,
  // but the test avoids that singleton to prevent cross-case global state.
  router.beforeEach(onboardingGuard);
  return router;
}

/** Stub the GET /api/onboarding/status return value read by isOnboardingDone inside the guard. */
function stubOnboardingStatus(done: boolean): void {
  (globalThis as Record<string, unknown>).fetch = async () => ({
    ok: true,
    status: 200,
    json: async () => ({ code: 0, data: { done } }),
  });
}

const suite: TestSuite = {
  name: "Onboarding 路由守卫（/welcome 强制跳转）",
  cases: [
    ["未完成引导：访问首页被重定向到 /welcome", async () => {
      resetOnboardingCache();
      stubOnboardingStatus(false);
      const router = guardedRouter();
      await router.push("/");
      assertEqual(router.currentRoute.value.path, "/welcome", "应落在欢迎页");
    }],

    ["未完成引导：访问孩子端地址也被重定向", async () => {
      resetOnboardingCache();
      stubOnboardingStatus(false);
      const router = guardedRouter();
      await router.push("/kid/subjects");
      assertEqual(router.currentRoute.value.path, "/welcome", "孩子端同样被拦");
    }],

    ["已完成引导：访问首页放行", async () => {
      resetOnboardingCache();
      stubOnboardingStatus(true);
      const router = guardedRouter();
      await router.push("/exams");
      assertEqual(router.currentRoute.value.path, "/exams", "应到达目标路由");
    }],

    ["直访 /welcome 不会二次重定向（防握手循环）", async () => {
      resetOnboardingCache();
      stubOnboardingStatus(false);
      const router = guardedRouter();
      await router.push("/welcome");
      assertEqual(router.currentRoute.value.path, "/welcome", "原地停留");
      assert(router.currentRoute.value.matched.length > 0, "已命中欢迎路由，非 404");
    }],
  ],
};

export default suite;