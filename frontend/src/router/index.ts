import { createRouter, createWebHistory, type RouteLocationNormalized, type RouteRecordRaw } from "vue-router";
import { markRouteStart } from "../lib/renderTimer";
import { isOnboardingDone } from "../lib/onboarding";
import { readParentToken } from "../lib/parentSession";
import { readKidToken } from "../lib/kidSession";

/** Exported for tests; the default export is still the router instance. */
export const routes: RouteRecordRaw[] = [
  {
    path: "/",
    component: () => import("../layouts/ParentLayout.vue"),
    // Marked audience=parent so parentGuard blocks unauthenticated access.
    meta: { audience: "parent" },
    children: [
      { path: "", name: "home", component: () => import("../pages/HomePage.vue") },
      { path: "exams", name: "exam-list", component: () => import("../pages/ExamListPage.vue") },
      { path: "exams/upload", name: "exam-create", component: () => import("../pages/ExamCreatePage.vue") },
      { path: "exams/:id", name: "exam-viewer", component: () => import("../pages/ExamViewerPage.vue") },
      { path: "kids", name: "kids", component: () => import("../pages/KidsPage.vue") },
      { path: "kids/:id", name: "kid-detail", component: () => import("../pages/KidDetailPage.vue") },
      { path: "storage", name: "storage", component: () => import("../pages/settings/StoragePage.vue") },
      { path: "image", name: "image-settings", component: () => import("../pages/settings/ImageSettingsPage.vue") },
      { path: "kids-auth", name: "kids-auth", component: () => import("../pages/settings/KidsAuthPage.vue") },
      { path: "parents", name: "parents", component: () => import("../pages/settings/ParentsPage.vue") },
      { path: "tasks", name: "tasks-settings", component: () => import("../pages/settings/TasksPage.vue") },
      { path: "subjects", name: "subjects", component: () => import("../pages/settings/SubjectsPage.vue") },
      { path: "knowledge-points", name: "knowledge-points", component: () => import("../pages/settings/KnowledgePointsPage.vue") },
      { path: "system", name: "system", component: () => import("../pages/settings/SystemPage.vue") },
    ],
  },
  {
    // Kid side reuses the parent page components (no copies, no role branching), prefix only.
    //
    // Mounted under `/kid`, not `/kid/login`: the login page is a standalone entry (same shape
    // as parent /login) and must live outside KidLayout. Static segments match before the
    // dynamic `/kid/:accessCode`, so the three groups coexist without conflict.
    //
    // Credentials never appear in the URL (kid JWT, wired in front end); old code-bearing
    // `/kid/:code*` links redirect to the invite page (see kid-entry).
    path: "/kid",
    component: () => import("../layouts/KidLayout.vue"),
    children: [
      { path: "", name: "kid-home", component: () => import("../pages/KidHomePage.vue"), meta: { audience: "kid" } },
      { path: "exams", name: "kid-exams", component: () => import("../pages/ExamListPage.vue"), meta: { audience: "kid" } },
      { path: "exams/upload", name: "kid-exam-create", component: () => import("../pages/ExamCreatePage.vue"), meta: { audience: "kid" } },
      { path: "exams/:id", name: "kid-exam-viewer", component: () => import("../pages/ExamViewerPage.vue"), meta: { audience: "kid" } },
      { path: "subjects", name: "kid-subjects", component: () => import("../pages/settings/SubjectsPage.vue"), meta: { audience: "kid" } },
      {
        path: "knowledge-points",
        name: "kid-knowledge-points",
        component: () => import("../pages/settings/KnowledgePointsPage.vue"),
        meta: { audience: "kid" },
      },
    ],
  },
  {
    // Old shared kid link (`/kid/:accessCode`, access_code = invite code) -> invite page
    // set-PIN flow. The URL never carries the session credential (kid_token holds it).
    path: "/kid/:accessCode",
    name: "kid-entry",
    redirect: (to) => `/invite/${encodeURIComponent(String(to.params.accessCode ?? ""))}`,
  },
  {
    // Old deep links (`/kid/abc/exams`) -> invite page too; the deep target cannot be honored
    // in a session-less redirect, so post-PIN navigation resumes from /kid home.
    path: "/kid/:accessCode/:rest(.*)*",
    name: "kid-entry-deep",
    redirect: (to) => `/invite/${encodeURIComponent(String(to.params.accessCode ?? ""))}`,
  },
  {
    // Standalone (no Layout). onboardingGuard forces navigation here until setup is done.
    path: "/welcome",
    name: "welcome",
    component: () => import("../pages/WelcomePage.vue"),
  },
  {
    // Standalone. parentGuard funnels unauthenticated visitors here with a redirect query.
    path: "/login",
    name: "login",
    component: () => import("../pages/LoginPage.vue"),
  },
  {
    // Standalone. The kid invite link (/invite/<access_code>) lands here: validate code, set
    // PIN, write kid token, go to /kid.
    path: "/invite/:code",
    name: "invite",
    component: () => import("../pages/InvitePage.vue"),
  },
  {
    // Standalone (top-level, not a /kid child: KidLayout would flash a top bar before login).
    // kidGuard sends token-less kid devices here.
    path: "/kid/login",
    name: "kid-login",
    component: () => import("../pages/KidLoginPage.vue"),
  },
  { path: "/:pathMatch(.*)*", redirect: "/" },
];

const router = createRouter({ history: createWebHistory(), routes });

/**
 * onboardingGuard — first-run setup guard (exported for tests).
 *
 * Mirrors the backend /onboarding/status view: done -> pass, not done -> /welcome,
 * request failed -> **pass** (this is onboarding, not auth; locking users in the wizard
 * when the backend stutters is worse than an empty shell).
 */
export async function onboardingGuard(to: RouteLocationNormalized): Promise<true | { path: string }> {
  if (to.path === "/welcome") return true;
  const done = await isOnboardingDone();
  if (done === false) return { path: "/welcome" };
  return true;
}

/**
 * parentGuard — parent-side login interception (exported for tests).
 *
 * Only checks that the parent JWT is **present** (synchronous, no signature check): a bad
 * token is cleared by authRequest on the first 40100 and bounced back. The backend judges
 * "is it valid", so the guard doesn't duplicate signature verification.
 *
 * Exempt: /login and /welcome; the whole /kid/* subtree (kid JWT, unrelated to the parent
 * token); and shell-less pages not marked audience=parent (they handle auth themselves).
 */

/**
 * isKidPath — true for exactly `/kid` or a `/kid/*` subpath.
 *
 * Not `startsWith("/kid")`: `/kids`, `/kids/:id`, `/kids-auth` all start with it and would
 * be misjudged, sending a parent in "Settings -> Kids & passwordless access" home.
 */
function isKidPath(path: string): boolean {
  return path === "/kid" || path.startsWith("/kid/");
}

/**
 * kidGuard — kid-side interception, keyed on two localStorage tokens, **parent first**:
 *   - /login, /welcome, /invite/*: pass first (credential-independent; blocking them makes
 *     the invite and login pages kick each other around);
 *   - parent token present -> parent device: send it back to `/` unless already off /kid/*;
 *   - kid token only -> kid device: /kid/login passes (no self-loop), /kid/* passes with a
 *     token / goes to /kid/login without, parent routes go to /kid;
 *   - neither -> pass, parentGuard decides by audience.
 *
 * Parent token must win over "a kid session sends non-/kid into /kid", or the two rules
 * loop forever between `/` and `/kid`, spamming /api/onboarding/status each hop.
 */
export function kidGuard(to: RouteLocationNormalized): true | { path: string } {
  // Entry and invite pages: credential-independent, pass first.
  if (to.path === "/login" || to.path === "/welcome" || to.path.startsWith("/invite")) {
    return true;
  }

  const hasParentToken = readParentToken() !== "";
  const hasKidToken = readKidToken() !== "";

  if (isKidPath(to.path)) {
    // A parent device does not enter the kid side (including entries like /kid/login).
    if (hasParentToken) return { path: "/" };
    // The kid login page itself: the landing point without a session, must pass (prevents self-loop).
    if (to.path === "/kid/login") return true;
    // No kid session -> send to the kid login page.
    if (!hasKidToken) return { path: "/kid/login" };
    return true;
  }

  // Parent device: stay on the parent side regardless of any leftover kid session.
  if (hasParentToken) return true;

  // Kid device: visiting a parent-side route -> send back to the kid side.
  if (hasKidToken) return { path: "/kid" };
  return true;
}

export function parentGuard(to: RouteLocationNormalized): true | { path: string; query: { redirect: string } } {
  if (to.path === "/login" || to.path === "/welcome") return true;
  // Must match the kid side precisely: startsWith("/kid") would treat /kids-auth as a kid page
  // and let parent-side kid management/passwordless auth bypass this guard when unauthenticated
  // (the same pitfall as kidGuard).
  if (isKidPath(to.path)) return true;
  if (!to.matched.some((r) => r.meta.audience === "parent")) return true;
  if (readParentToken() !== "") return true;
  return { path: "/login", query: { redirect: to.fullPath } };
}

// Recorded before any guard: guard awaits (onboarding hits an endpoint) count as navigation,
// and missing them would undercount render time.
router.beforeEach(markRouteStart);
router.beforeEach(onboardingGuard);
router.beforeEach(kidGuard);
router.beforeEach(parentGuard);

export default router;
