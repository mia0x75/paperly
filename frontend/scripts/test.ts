/**
 * Frontend test orchestration:
 *   1. Bun.build bundles a lane entry (test/run[-unit|-component|-page].ts;
 *      .vue via scripts/vue-plugin.ts) -> .test-build/run.js
 *   2. bun executes the bundle, exit code decides pass/fail
 *
 * Same path as scripts/build.ts: no Vite; `.vue` is still compiled by the
 * official @vue/compiler-sfc. happy-dom globals are injected by import order
 * (see test/setup.ts).
 *
 * Lanes (argv[2], default all): unit = pure-TS suites (no .vue in the graph,
 * run directly without bundling); component = reusable .vue pieces; page =
 * .vue pages. Lanes are separate OS processes with their own globals, so they
 * may run concurrently; suites inside one process stay sequential (shared
 * document / localStorage / fetch stub).
 */
import { rm } from "node:fs/promises";
import { vuePlugin } from "./vue-plugin";

const ROOT = new URL("..", import.meta.url).pathname;
const OUTDIR = `${ROOT}.test-build`;

const LANE_ENTRIES: Record<string, string> = {
  all: "run.ts",
  unit: "run-unit.ts",
  component: "run-component.ts",
  page: "run-page.ts",
};

const lane = process.argv[2] ?? "all";
const entry = LANE_ENTRIES[lane];
if (entry === undefined) {
  console.error(`unknown test lane ${JSON.stringify(lane)} (want all|unit|component|page)`);
  process.exit(1);
}

// The unit lane has no .vue in its import graph: run it directly, skipping
// the bundle step entirely (seconds instead of a full build).
if (lane === "unit") {
  const proc = Bun.spawn([process.execPath, `${ROOT}test/${entry}`], {
    cwd: ROOT,
    stdout: "inherit",
    stderr: "inherit",
  });
  process.exit(await proc.exited);
}

await rm(OUTDIR, { recursive: true, force: true });

const result = await Bun.build({
  entrypoints: [`${ROOT}test/${entry}`],
  outdir: OUTDIR,
  target: "bun",
  plugins: [vuePlugin()],
  define: {
    __VUE_OPTIONS_API__: "true",
    __VUE_PROD_DEVTOOLS__: "false",
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: "false",
  },
});
if (!result.success) {
  for (const log of result.logs) console.error(log);
  process.exit(1);
}

const outName = entry.replace(/\.ts$/, ".js");
const proc = Bun.spawn([process.execPath, `${OUTDIR}/${outName}`], {
  cwd: ROOT,
  stdout: "inherit",
  stderr: "inherit",
});
process.exit(await proc.exited);
