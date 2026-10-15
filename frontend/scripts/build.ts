/**
 * Paperly frontend build orchestration:
 *   1. Tailwind CLI compiles src/styles/app.css -> src/styles/app.build.css
 *   2. Bun.build bundles index.html + Vue SFCs -> ../internal/web/dist (go:embed target)
 *
 * Everything runs on Bun (frozen in TECH_STACK), no Vite.
 * `.vue` is compiled by scripts/vue-plugin.ts via the official @vue/compiler-sfc.
 */
import { mkdir, rm, writeFile } from "node:fs/promises";
import { vuePlugin } from "./vue-plugin";

const ROOT = new URL("..", import.meta.url).pathname;
const OUTDIR = `${ROOT}../internal/web/dist`;

async function run(cmd: string[]): Promise<void> {
  const proc = Bun.spawn(cmd, {
    cwd: ROOT,
    stdout: "inherit",
    stderr: "inherit",
  });
  const code = await proc.exited;
  if (code !== 0) {
    throw new Error(`command failed (exit ${code}): ${cmd.join(" ")}`);
  }
}

// Wipe the output dir first so no stale chunks get embedded by go:embed.
// Keep .gitkeep: it lets `go:embed all:dist` compile even before a build (see internal/web/web.go).
await rm(OUTDIR, { recursive: true, force: true });
await mkdir(OUTDIR, { recursive: true });
await writeFile(`${OUTDIR}/.gitkeep`, "");

// 1. Tailwind build (scans index.html and all .vue/.ts under src).
// Call the CLI's JS entry directly (not `bun x`) to avoid parse failures in spawn / non-TTY environments.
const tailwindBin = new URL("../node_modules/@tailwindcss/cli/dist/index.mjs", import.meta.url).pathname;
await run([
  process.execPath,
  tailwindBin,
  "-i",
  "./src/styles/app.css",
  "-o",
  "./src/styles/app.build.css",
  "--minify",
]);

// 2. Bundle the frontend (Bun.build + Vue SFC plugin).
// publicPath="/": assets use absolute paths so the SPA fallback (e.g. /kids/123) loads correctly.
//
// The three Vue compile-time feature flags must be injected by the bundler: at runtime, Vue
// warns when they are undefined and patches globalThis on the fly — which kills tree-shaking
// (devtools code stays in the bundle) and makes bare identifiers like `if (__VUE_OPTIONS_API__)`
// depend on the implicit timing of "patch happens before evaluation". Values match Vite's defaults:
//   - __VUE_OPTIONS_API__=true: keeps mixin/global-mixin support for consumers that rely on it;
//   - __VUE_PROD_DEVTOOLS__=false: no devtools in production;
//   - __VUE_PROD_HYDRATION_MISMATCH_DETAILS__=false: SSR-only, Paperly is a pure SPA.
const result = await Bun.build({
  entrypoints: [`${ROOT}index.html`],
  outdir: OUTDIR,
  target: "browser",
  minify: true,
  publicPath: "/",
  define: {
    __VUE_OPTIONS_API__: "true",
    __VUE_PROD_DEVTOOLS__: "false",
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: "false",
  },
  plugins: [vuePlugin()],
});
if (!result.success) {
  for (const log of result.logs) {
    console.error(log);
  }
  throw new Error("frontend build failed");
}

for (const output of result.outputs) {
  console.log(`  ${output.path.replace(`${ROOT}../`, "")}  ${(output.size / 1024).toFixed(2)} KB`);
}
console.log(`\n构建完成 → ${OUTDIR}`);
