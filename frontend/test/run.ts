/**
 * test/run.ts -- test entry point.
 *
 * Statically imports each test file, runs the cases and aggregates. Any failure
 * exits 1 for CI / self-check scripts.
 *
 * Executed by scripts/test.ts bundled with Bun.build + vuePlugin, same as
 * scripts/build.ts: bun needs that to load .vue.
 */
import type { TestSuite } from "./assert";
import { runSuites } from "./driver";
import homePageSuite from "./home.page.test";
import examListPageSuite from "./exam-list.page.test";
import examViewerPageSuite from "./exam-viewer.page.test";
import examCreatePageSuite from "./exam-create.page.test";
import kidsPageSuite from "./kids.page.test";
import kidDetailPageSuite from "./kid-detail.page.test";
import kidHomePageSuite from "./kid-home.page.test";
import examTitleSuite from "./exam-title.test";
import layoutHeaderSuite from "./layout-header.test";
import messageBellSuite from "./message-bell.test";
import settingsPageSuite from "./settings.page.test";
import settingsRoutesSuite from "./settings-routes.test";
import settingsMenuSuite from "./settings-menu.test";
import kidSettingsRoutesSuite from "./kid-settings-routes.test";
import onboardingRoutesSuite from "./onboarding-routes.test";
import kidTopbarSuite from "./kid-topbar.test";
import welcomePageSuite from "./welcome.page.test";
import overlayOutsideClickSuite from "./overlay-outside-click.test";
import modalSuite from "./modal.test";
import chartSuite from "./chart.test";
import buttonVariantSuite from "./button-variant.test";
import buttonUnifiedSuite from "./button-unified.test";
import messageKindSuite from "./message-kind.test";
import tokensSuite from "./tokens.test";
import parentSessionSuite from "./parent-session.test";
import parentGuardSuite from "./parent-guard.test";
import loginPageSuite from "./login.page.test";
import invitePageSuite from "./invite.page.test";
import kidLoginPageSuite from "./kid-login.page.test";
import guardLoopSuite from "./guard-loop.test";
import clientAuthSuite from "./client-auth.test";
import clientTimeoutSuite from "./client-timeout.test";
import formatSuite from "./format.test";
import parentsPageSuite from "./parents.page.test";
import storageTrendSuite from "./storage-trend.test";
import storageTrendCardSuite from "./storage-trend-card.test";
import renderTimerSuite from "./render-timer.test";
import summarySuite from "./summary.test";
import subjectVisibleSuite from "./subject-visible.test";
import taskSuite from "./task.test";

const SUITES: TestSuite[] = [
  homePageSuite,
  examListPageSuite,
  examViewerPageSuite,
  examCreatePageSuite,
  kidsPageSuite,
  kidDetailPageSuite,
  kidHomePageSuite,
  examTitleSuite,
  layoutHeaderSuite,
  messageBellSuite,
  settingsPageSuite,
  settingsRoutesSuite,
  settingsMenuSuite,
  kidSettingsRoutesSuite,
  onboardingRoutesSuite,
  kidTopbarSuite,
  welcomePageSuite,
  overlayOutsideClickSuite,
  modalSuite,
  chartSuite,
  buttonVariantSuite,
  buttonUnifiedSuite,
  messageKindSuite,
  tokensSuite,
  parentSessionSuite,
  parentGuardSuite,
  guardLoopSuite,
  clientAuthSuite,
  clientTimeoutSuite,
  formatSuite,
  loginPageSuite,
  invitePageSuite,
  kidLoginPageSuite,
  parentsPageSuite,
  storageTrendSuite,
  storageTrendCardSuite,
  renderTimerSuite,
  summarySuite,
  subjectVisibleSuite,
  taskSuite,
];

await runSuites(SUITES);
