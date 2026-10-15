# 前端测试骨架

happy-dom 挂载 + 极简断言。**零新增框架**，只多一个 devDep（happy-dom）。

```bash
bun run test        # 跑全部用例，失败退出码 1
```

## 为什么不用 vitest

评估过，结论是**不值得**：

- vitest 会带进 vite / esbuild 等一批传递依赖。Paperly 定位是 Box 单机工具、极简交付
  （AGENTS.md 红线 F5），为几百个断言换一整套 Vite 工具链不成比例。
- 构建侧已是「Bun.build + 自写 vue-plugin」的极简链路（见 `scripts/build.ts`），
  测试侧再引入 Vite 会出现**两套打包配置**，以后调 SFC 编译行为要改两处。
- 需要的能力只有三件：把组件挂到 DOM、喂一个假 fetch、断言文本与节点，
  `mount.ts` + `assert.ts` 就覆盖了。

将来真需要测试发现 / 并行 / 快照 / CI 矩阵时再换 vitest 不迟——测试文件只需改 import 头
（`./assert` → `vitest`），用例体不用动。

## 目录

| 文件 | 职责 |
|---|---|
| `setup.ts` | happy-dom DOM 环境 + 全局安装（**必须最先 import**，见下）；`resetDOM()` 清 body + localStorage/sessionStorage，避免选中学生等状态跨用例泄漏；另安装 canvas 2D 与 ResizeObserver 桩（Chart.js 需要） |
| `mount.ts` | `mountPage(组件, {responses, path, storage})` → `{text, doc, requests, calls, errors, navigations}`；`text` 是 getter（交互后现取），`calls` 含 method/JSON body/multipart form。另导出 `flush()`。默认装内存路由（可 `router:false` 关掉）与 Pinia。`responses` 的键可写 `"POST /api/kids"` 限定方法（同路径 GET/POST 响应不同）；响应体带 `code` 字段则原样返回，否则自动包成 `{code:0, data:值}` |
| `assert.ts` | `assert` / `assertEqual` / `assertIncludes` / `assertNotIncludes`，另导出 `TestSuite` / `TestCase` 类型 |
| `run.ts` | 入口：静态 import 各 `*.test.ts`，汇总并按退出码判定 |
| `controls.ts` | 测试辅助：模拟点击 / 输入 / 选择交互 |
| `home.page.test.ts` | 首页（家长端）六卡 + 孩子端用例，同时验证骨架自身可用 |
| `exam-list.page.test.ts` | 试卷列表页用例（筛选、翻页、状态过滤） |
| `exam-viewer.page.test.ts` | 试卷详情页用例（`calls` 断言 PUT/DELETE 的 method 与 JSON 载荷、页级操作） |
| `exam-create.page.test.ts` | 试卷上传 / 新建用例（类型校验、multipart 字段、补写 PUT、失败与重复分支） |
| `exam-title.test.ts` | 试卷标题生成规则用例 |
| `kids.page.test.ts` | 学生列表用例（状态/年级筛选、重置、空态、进入详情） |
| `kid-detail.page.test.ts` | 学生详情用例（整表保存、设为当前、该生试卷带参跳转、二次确认删除） |
| `kid-home.page.test.ts` | 孩子端首页用例 |
| `kid-login.page.test.ts` / `invite.page.test.ts` / `login.page.test.ts` | 孩子登录 / 邀请 / 家长登录用例 |
| `kid-topbar.test.ts` / `kid-settings-routes.test.ts` / `onboarding-routes.test.ts` | 路由与导航守卫用例 |
| `parent-guard.test.ts` / `guard-loop.test.ts` / `client-auth.test.ts` / `parent-session.test.ts` / `tokens.test.ts` | 鉴权与 token 用例（JWT 持久化、守卫、401 收敛） |
| `settings.page.test.ts` / `settings-menu.test.ts` / `settings-routes.test.ts` / `parents.page.test.ts` | 设置页用例 |
| `welcome.page.test.ts` | 首启向导用例 |
| `layout-header.test.ts` / `message-bell.test.ts` | 布局头 / 通知铃用例 |
| `overlay-outside-click.test.ts` / `chart.test.ts` / `button-variant.test.ts` / `storage-trend.test.ts` / `render-timer.test.ts` | 放大层外点关闭 / 图表 / 按钮变体 / 存储趋势 / 渲染计时用例 |

用例清单以 `run.ts` 的 `SUITES` 数组为准（新增用例需在此登记）。

## 两个非平凡陷阱（必读）

两者都**不会**报出「测试写错了」，而是给出与真实病因无关的报错，所以在这里留档。

### 1. import 顺序是硬约束

vue 的 `runtime-dom` 在**模块顶层**读全局 `document`：

```js
const doc = typeof document !== 'undefined' ? document : null
```

这行在 vue 模块体执行时就跑完了。若那时 `globalThis.document` 还是 `undefined`，
vue 内部的 `doc` 会**永久**是 `null`，之后报：

```
TypeError: null is not an object (evaluating 'doc.createElement')
```

这跟挂载点、组件写法都无关，极难定位。ESM 按 import 声明顺序求值依赖，
只要 harness 排在 vue 与业务 SFC **之前** import 就安全：

```ts
import { mountPage } from "./mount";                    // ① 必须在前
import ExamListPage from "../src/pages/ExamListPage.vue"; // ② SFC 在后
```

`mount.ts` 已把 `"./setup"` 放在自身首个 import 位，并提供 `assertDOMReady()`
前置自检，把这个错误挡在断言之前。

### 2. 挂载点必须 append 进 `document.body`

`createElement("div")` 出来的节点若不 append，渲染确实发生了，但 `document.body`
里查不到任何节点 —— 于是 `textContent` 与 `querySelector` 全为空，看起来像
「组件完全没渲染」。`mountPage()` 已处理。

## 写一个新用例

```ts
// test/example.page.test.ts（示意；真实用例见 exam-list.page.test.ts）
import { assertEqual, type TestSuite } from "./assert";
import { mountPage } from "./mount";
import ExamListPage from "../src/pages/ExamListPage.vue";

const suite: TestSuite = {
  name: "ExamplePage（列表）",
  cases: [
    ["空态提示", async () => {
      const r = await mountPage(ExamListPage, {
        responses: { "/api/exams": { items: [], next_cursor: 0, has_more: false } },
      });
      assertEqual(r.requests.length, 1, "只应请求一次列表");
    }],
  ],
};
export default suite;
```

然后在 `run.ts` 里 `import` 并在 `SUITES` 数组里登记。
`responses` 的值不含 `code` 字段时会自动包成 `{code:0,data:值}`；
要模拟错误就显式传 `{code: 50001, message: "..."}`。
