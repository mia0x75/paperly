/**
 * test/run-component.ts -- reusable-component lane (imports .vue, needs bundling).
 *
 * Entry for `bun run scripts/test.ts component`: bundled with Bun.build + vuePlugin
 * like the full run, then run as its own process.
 */
import { runSuites } from "./driver";
import type { TestSuite } from "./assert";
import buttonVariantSuite from "./button-variant.test";
import buttonUnifiedSuite from "./button-unified.test";
import layoutHeaderSuite from "./layout-header.test";
import messageBellSuite from "./message-bell.test";
import overlayOutsideClickSuite from "./overlay-outside-click.test";
import modalSuite from "./modal.test";
import settingsMenuSuite from "./settings-menu.test";
import storageTrendCardSuite from "./storage-trend-card.test";

const SUITES: TestSuite[] = [
  buttonVariantSuite,
  buttonUnifiedSuite,
  layoutHeaderSuite,
  messageBellSuite,
  overlayOutsideClickSuite,
  modalSuite,
  settingsMenuSuite,
  storageTrendCardSuite,
];

await runSuites(SUITES);
