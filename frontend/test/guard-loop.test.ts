/**
 * test/guard-loop.test.ts -- guard chain infinite-loop prevention.
 *
 * With a parent token and a kid token coexisting, two mutually inverse guard rules
 * -- "has session -> anything not /kid goes to /kid" and "has parent token -> /kid
 * goes to /" -- redirect infinitely between `/` and `/kid`. Each hop fires a GET
 * /api/onboarding/status, so navigation never converges (white screen).
 *
 * Uses the real guards + route table to simulate the navigation chain, pinning:
 *   1. both tokens -> home converges, no redirect;
 *   2. both, starting from /kid -> converges to /, no bounce;
 *   3. kid token only -> anything not /kid is sent to /kid;
 *   4. parent token only -> /kid is still sent away;
 *   5. no token at /kid/* -> sent to /kid/login, converges;
 *   6. `/kids*` (/kids, /kids-auth) must not be misread as kid-side by
 *      `startsWith("/kid")`.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { onboardingGuard, kidGuard, parentGuard, routes } from "../src/router/index";
import { PARENT_TOKEN_KEY } from "../src/lib/parentSession";
import { KID_TOKEN_KEY } from "../src/lib/kidSession";
import { resetOnboardingCache } from "../src/lib/onboarding";
import { createMemoryHistory, createRouter, type RouteLocationNormalized, type RouteRecordRaw } from "vue-router";

/**
 * Clear localStorage + onboarding cache before each case: a failed case's residue
 * pollutes later cases, and isOnboardingDone caches its final state across cases
 * (without resetting, a done=false case can't reach /welcome).
 */
function resetSession(): void {
  localStorage.clear();
  localStorage.removeItem(PARENT_TOKEN_KEY);
  localStorage.removeItem(KID_TOKEN_KEY);
  resetOnboardingCache();
}

/**
 * One hop of the simulated navigation chain: null = allowed to that path;
 * string = redirected by a guard.
 */
type Hop = string | null;

let statusCalls = 0;

function stubStatus(done: boolean): void {
  statusCalls = 0;
  (globalThis as Record<string, unknown>).fetch = async () => {
    statusCalls++;
    return { ok: true, status: 200, json: async () => ({ code: 0, data: { done } }) };
  };
}

/**
 * Build a resolver from the real route table with no guards attached
 * (simulateNav runs the guards step by step).
 */
function bareRouter(): ReturnType<typeof createRouter> {
  const Empty = { render: () => null };
  return createRouter({
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
}

/**
 * Run the guard chain step by step per vue-router semantics: if any guard returns
 * a redirect, cancel the current navigation and restart the whole chain at the
 * target path. Returns the sequence of visited paths.
 */
async function simulateNav(startPath: string, maxHops: number): Promise<string[]> {
  const router = bareRouter();
  const visited: string[] = [];
  let path = startPath;
  for (let i = 0; i < maxHops; i++) {
    visited.push(path);
    const to = router.resolve(path) as RouteLocationNormalized;
    let hop: Hop = null;
    for (const guard of [onboardingGuard, kidGuard, parentGuard]) {
      const res = await guard(to);
      if (res !== true) {
        hop = res.path;
        break;
      }
    }
    if (hop === null) return visited;
    path = hop;
  }
  return visited;
}

/** A short path sequence counts as converged. */
function converged(visited: string[]): boolean {
  return visited.length <= 6;
}

const suite: TestSuite = {
  name: "守卫链防死循环（家长 token + 孩子码并存）",
  cases: [
    ["家长 token + 孩子 token 并存：访问首页收敛，不进 /kid 循环", async () => {
      resetSession();
      localStorage.setItem(PARENT_TOKEN_KEY, "jwt-parent");
      localStorage.setItem(KID_TOKEN_KEY, "jwt-kid");
      stubStatus(true);
      const visited = await simulateNav("/", 40);
      assert(converged(visited), `守卫链未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/", "应停在首页");
      assert(statusCalls <= 2, `status 不该被反复调用（实际 ${statusCalls} 次）`);
    }],

    ["并存时从 /kid 出发：收敛到 /，不回弹", async () => {
      resetSession();
      localStorage.setItem(PARENT_TOKEN_KEY, "jwt-parent");
      localStorage.setItem(KID_TOKEN_KEY, "jwt-kid");
      stubStatus(true);
      const visited = await simulateNav("/kid", 40);
      assert(converged(visited), `守卫链未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/", "家长设备应停在家长端首页");
    }],

    ["只有孩子 token：非 /kid 仍被送到 /kid（原规则不回退）", async () => {
      resetSession();
      localStorage.setItem(KID_TOKEN_KEY, "jwt-kid");
      stubStatus(true);
      const visited = await simulateNav("/exams", 40);
      assert(converged(visited), `守卫链未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/kid", "孩子设备应进入孩子端");
    }],

    ["无任何 token：/kid/* 被送到 /kid/login 并收敛", async () => {
      resetSession();
      stubStatus(true);
      for (const start of ["/kid", "/kid/exams", "/kid/subjects"]) {
        const visited = await simulateNav(start, 40);
        assert(converged(visited), `${start} 未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
        assertEqual(visited[visited.length - 1]!, "/kid/login", `${start} 应落在孩子登录页`);
      }
    }],

    ["孩子 token 访问 /kid/login 原地停留（不自跳环）", async () => {
      resetSession();
      localStorage.setItem(KID_TOKEN_KEY, "jwt-kid");
      stubStatus(true);
      const visited = await simulateNav("/kid/login", 40);
      assert(converged(visited), `未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/kid/login", "登录页自身应放行");
    }],

    ["无 token：/invite/:code 与 /login 入口页原地停留", async () => {
      resetSession();
      stubStatus(true);
      for (const start of ["/invite/abc123", "/login", "/welcome"]) {
        const visited = await simulateNav(start, 40);
        assertEqual(
          visited[visited.length - 1]!,
          start,
          `${start} 是凭据无关的入口页，不应被改道`,
        );
      }
    }],

    ["只有家长 token：/kid 仍被送出（原规则不回退）", async () => {
      resetSession();
      localStorage.setItem(PARENT_TOKEN_KEY, "jwt-parent");
      stubStatus(true);
      const visited = await simulateNav("/kid", 40);
      assert(converged(visited), `守卫链未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/", "家长设备不进孩子端");
    }],

    ["家长 token：/kids-auth 与 /kids 原地放行（不被 /kid 前缀误判）", async () => {
      resetSession();
      localStorage.setItem(PARENT_TOKEN_KEY, "jwt-parent");
      stubStatus(true);
      for (const start of ["/kids-auth", "/kids", "/kids/5"]) {
        const visited = await simulateNav(start, 40);
        assert(converged(visited), `${start} 未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
        assertEqual(visited[visited.length - 1]!, start, `${start} 应原地放行`);
      }
    }],

    ["孩子 token：/kids-auth 送回孩子端（家长端路由不外泄）", async () => {
      resetSession();
      localStorage.setItem(KID_TOKEN_KEY, "jwt-kid");
      stubStatus(true);
      const visited = await simulateNav("/kids-auth", 40);
      assert(converged(visited), `未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/kid", "孩子设备应被送回孩子端");
    }],

    ["无 token：/kids-auth 被 parentGuard 送到 /login（精确前缀才认得 audience）", async () => {
      resetSession();
      stubStatus(true);
      const visited = await simulateNav("/kids-auth", 40);
      assert(converged(visited), `未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/login", "未登录家长应被拦到登录页");
    }],

    ["引导未完成：首页被送到 /welcome 后原地停留", async () => {
      resetSession();
      stubStatus(false);
      const visited = await simulateNav("/", 40);
      assert(converged(visited), `守卫链未收敛（${visited.length} 跳）：${visited.join(" → ")}`);
      assertEqual(visited[visited.length - 1]!, "/welcome", "应停在引导页");
    }],
  ],
};

export default suite;
