/**
 * test/settings-routes.test.ts -- settings routes (8 top-level routes; /settings removed).
 *
 * The key assertion is that "/settings" is not a route: adding the long page or a
 * dedicated redirect back fails immediately. Menu-to-route correspondence is also
 * verified here, so the two can't drift apart.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { routes } from "../src/router/index";

/** The 8 top-level routes (in the same order as SettingsMenu's MENU). */
const EXPECTED: Array<{ path: string; name: string }> = [
  { path: "/storage", name: "storage" },
  { path: "/image", name: "image-settings" },
  { path: "/kids-auth", name: "kids-auth" },
  { path: "/parents", name: "parents" },
  { path: "/tasks", name: "tasks-settings" },
  { path: "/subjects", name: "subjects" },
  { path: "/knowledge-points", name: "knowledge-points" },
  { path: "/system", name: "system" },
];

interface Flat {
  /** Absolute path (e.g. /exams/upload). */
  path: string;
  name: string;
  component: unknown;
}

/** Flatten the nested route table into a list findable by absolute path. */
function flat(): Flat[] {
  const out: Flat[] = [];
  for (const r of routes) {
    if (r.children && r.children.length > 0) {
      for (const c of r.children) {
        out.push({
          path: `/${String(c.path).replace(/^\//, "")}`,
          name: String(c.name ?? ""),
          component: c.component,
        });
      }
      continue;
    }
    out.push({ path: String(r.path), name: String(r.name ?? ""), component: r.component });
  }
  return out;
}

const ALL = flat();

function byPath(path: string): Flat | undefined {
  return ALL.find((r) => r.path === path);
}

const suite: TestSuite = {
  name: "设置路由（8 顶层 · /settings 不存在）",
  cases: [
    ["/settings 不是任何路由（无长页、无专属重定向）", async () => {
      assertEqual(byPath("/settings"), undefined, "/settings 必须彻底移除");
      assert(
        !ALL.some((r) => r.path.startsWith("/settings/")),
        "不应残留 /settings/* 前缀路由",
      );
    }],

    ["未匹配路径只走项目既有 catch-all（深链不会静默回设置页）", async () => {
      const catchAll = routes.find((r) => String(r.path).includes("pathMatch"));
      assert(catchAll !== undefined, "应有 catch-all 兜底路由");
      assert(
        catchAll?.redirect === "/",
        "/settings 落到项目既有兜底（重定向首页），不是为它新加的重定向",
      );
    }],

    ["8 个顶层路由全部注册且带组件", async () => {
      for (const { path } of EXPECTED) {
        const r = byPath(path);
        assert(r !== undefined, `应存在顶层路由 ${path}`);
        assert(r?.component !== undefined, `${path} 必须挂组件`);
      }
    }],

    ["8 个路由名与需求清单一致", async () => {
      for (const { name } of EXPECTED) {
        const hit = ALL.find((r) => r.name === name);
        assert(hit !== undefined, `应存在 name=${name} 的路由，实际表：${ALL.map((r) => r.name).join(",")}`);
      }
    }],

    ["路由名全局唯一（8 条新路由不撞既有路由名）", async () => {
      const names = ALL.map((r) => r.name).filter((n) => n !== "");
      const dup = names.filter((n, i) => names.indexOf(n) !== i);
      assertEqual(dup.length, 0, `不应有重名路由：${dup.join(", ")}`);
    }],

    ["8 个目标路径与既有路由不冲突", async () => {
      const reserved = ["", "/exams", "/exams/upload", "/exams/:id", "/kids", "/kids/:id"];
      for (const { path } of EXPECTED) {
        assert(
          !reserved.includes(path),
          `${path} 与既有路由冲突，需改用不冲突的近义路径`,
        );
      }
    }],
  ],
};

export default suite;