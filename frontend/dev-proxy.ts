/**
 * Paperly 开发代理：Bun.serve 提供构建产物 + /api 反向代理。
 *
 * 开发/生产同代码路径：dev 与 build 都走 scripts/build.ts 的同一套
 * Bun.build + Vue SFC 插件（Bun 原生不编译 .vue，故不能用 HTMLBundle dev server）。
 * 同源，零 CORS；前端永远只用相对 /api。
 *
 * 用法：bun run dev（默认 http://localhost:3000，代理到 http://localhost:8080）。
 * 修改前端后重新运行即可（或配 --watch 自行重启）。
 */

const ROOT = `${import.meta.dir}/`;
const PORT = Number(process.env.PORT ?? 3000);
const HOST = process.env.HOST ?? "localhost";
const API_TARGET = process.env.API_TARGET ?? "http://localhost:8080";
const DIST = `${ROOT}../internal/web/dist`;

// 首次构建（与 build 相同的编排）。
const buildProc = Bun.spawnSync([process.execPath, "run", `${ROOT}scripts/build.ts`], {
  cwd: ROOT,
  stdout: "inherit",
  stderr: "inherit",
});
if (buildProc.exitCode !== 0) {
  throw new Error(`前端构建失败（退出码 ${buildProc.exitCode}）`);
}
const server = Bun.serve({
  hostname: HOST,
  port: PORT,
  async fetch(req) {
    const url = new URL(req.url);
    if (url.pathname.startsWith("/api")) {
      return fetch(new Request(new URL(url.pathname + url.search, API_TARGET), req));
    }
    // 静态文件；未命中回退 index.html（SPA 路由）。
    const rel = url.pathname === "/" ? "index.html" : url.pathname.slice(1);
    const file = Bun.file(`${DIST}/${rel}`);
    if (await file.exists()) {
      return new Response(file);
    }
    return new Response(Bun.file(`${DIST}/index.html`), {
      headers: { "content-type": "text/html; charset=utf-8" },
    });
  },
});

console.log(`Paperly dev：http://${HOST}:${server.port}  →  API ${API_TARGET}`);
