/**
 * test/run-unit.ts -- pure-TS lane (no .vue imports anywhere in the graph).
 *
 * Runs directly with `bun test/run-unit.ts`: no bundling needed (vuePlugin is
 * only required to compile .vue SFCs). setup.ts is imported FIRST so happy-dom
 * globals exist before vue / vue-router module bodies execute (see test/setup.ts
 * import-order constraint).
 */
import "./setup";
import { runSuites } from "./driver";
import type { TestSuite } from "./assert";
import chartSuite from "./chart.test";
import clientAuthSuite from "./client-auth.test";
import clientTimeoutSuite from "./client-timeout.test";
import examTitleSuite from "./exam-title.test";
import formatSuite from "./format.test";
import guardLoopSuite from "./guard-loop.test";
import kidSettingsRoutesSuite from "./kid-settings-routes.test";
import messageKindSuite from "./message-kind.test";
import onboardingRoutesSuite from "./onboarding-routes.test";
import parentGuardSuite from "./parent-guard.test";
import parentSessionSuite from "./parent-session.test";
import renderTimerSuite from "./render-timer.test";
import settingsRoutesSuite from "./settings-routes.test";
import storageTrendSuite from "./storage-trend.test";
import subjectVisibleSuite from "./subject-visible.test";
import summarySuite from "./summary.test";
import taskSuite from "./task.test";
import tokensSuite from "./tokens.test";

const SUITES: TestSuite[] = [
  chartSuite,
  clientAuthSuite,
  clientTimeoutSuite,
  examTitleSuite,
  formatSuite,
  guardLoopSuite,
  kidSettingsRoutesSuite,
  messageKindSuite,
  onboardingRoutesSuite,
  parentGuardSuite,
  parentSessionSuite,
  renderTimerSuite,
  settingsRoutesSuite,
  storageTrendSuite,
  subjectVisibleSuite,
  summarySuite,
  taskSuite,
  tokensSuite,
];

await runSuites(SUITES);
