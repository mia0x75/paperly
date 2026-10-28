/**
 * test/run-page.ts -- page lane (imports .vue, needs bundling).
 *
 * Entry for `bun run scripts/test.ts page`: bundled with Bun.build + vuePlugin
 * like the full run, then run as its own process.
 */
import { runSuites } from "./driver";
import type { TestSuite } from "./assert";
import homePageSuite from "./home.page.test";
import examListPageSuite from "./exam-list.page.test";
import examViewerPageSuite from "./exam-viewer.page.test";
import examCreatePageSuite from "./exam-create.page.test";
import kidsPageSuite from "./kids.page.test";
import kidDetailPageSuite from "./kid-detail.page.test";
import kidHomePageSuite from "./kid-home.page.test";
import messageListPageSuite from "./message-list.page.test";
import kidTopbarSuite from "./kid-topbar.test";
import loginPageSuite from "./login.page.test";
import invitePageSuite from "./invite.page.test";
import kidLoginPageSuite from "./kid-login.page.test";
import welcomePageSuite from "./welcome.page.test";
import parentsPageSuite from "./parents.page.test";
import settingsPageSuite from "./settings.page.test";

const SUITES: TestSuite[] = [
  homePageSuite,
  examListPageSuite,
  examViewerPageSuite,
  examCreatePageSuite,
  kidsPageSuite,
  kidDetailPageSuite,
  kidHomePageSuite,
  messageListPageSuite,
  kidTopbarSuite,
  loginPageSuite,
  invitePageSuite,
  kidLoginPageSuite,
  welcomePageSuite,
  parentsPageSuite,
  settingsPageSuite,
];

await runSuites(SUITES);
