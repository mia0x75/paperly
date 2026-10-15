# AGENTS.md

> 面向 AI 编码 Agent 的协作契约。**指令式，非建议**。违反红线一律打回。
>
> 阅读顺序：本文件 → `DATABASE_DESIGN.md`（16 表权威源）→ `TECH_STACK.md`。

---

## 项目速览

**Paperly** — 面向中考备考的本地化试卷管理与成绩分析系统。

**战略前提（不得违背）**：
- 单机版优先（自托管）
- SQLite（modernc.org/sqlite，纯 Go 无 CGO），无多租户
- 单二进制交付（前端 `go:embed` 嵌入 Go）
- 离线可用（不依赖外部服务，核心功能可离线运行）

**当前阶段**：**v1.0.0 首次发布**。5 份文档（`REQUIREMENTS.md` / `TECH_STACK.md` / `DATABASE_DESIGN.md` / `API.md` / `ROUTES.md`）为权威源，本文件为 AI 协作契约。

---

## 核心原则

- **KISS**：优先简单、可维护的方案。Paperly 是单机工具，不是平台，不做过度设计。
- **事实优先**：先检查当前源码、配置、日志和运行状态，再引用历史结论。禁止凭记忆断言。
- **可验证目标**：性能优化必须有测量依据；无数据不做过度设计。
- **中文回复**：所有用户可见回复、方案和任务清单使用中文。

---

## 技术栈（不得偏离）

| 层 | 选型 |
|----|------|
| 后端语言 | Go 1.26+ |
| Web 框架 | Echo v5 |
| 数据库 | SQLite（modernc.org/sqlite） |
| 前端框架 | Vue 3.5+（`<script setup>`） |
| 状态管理 | Pinia 3 |
| 路由 | Vue Router 4 |
| 图表 | vue-chartjs 5 + Chart.js 4 |
| 样式 | Tailwind CSS 4 |
| UI 组件 | radix-vue 原语 + 手搓 shadcn 风格组件（`components/ui/`） |
| 构建 | Bun |
| HTTP 客户端 | fetch（原生，封装在 `frontend/src/api/client.ts`） |

**换栈唯一途径**：改 `TECH_STACK.md` 并由人类确认。AI 不得单方面换栈。
**权威源**：`TECH_STACK.md`。本表为速查，冲突以 `TECH_STACK.md` 为准。

---

## 工作流程

- **复杂任务**：调研 → 方案 → 确认 → 分解 → 执行 → 验证。
- **简单任务**：直接执行 + 必要验证。
- **外部依赖**：涉及外部库、API、CLI 或可能变化的行为时，**查阅最新官方文档和源码**，禁止凭记忆调用（AI 高频出错点）。
- **状态优先级**：当前源码与运行状态 > 历史 session / memory / 截图。

---

## 目录结构

```
paperly/
├── cmd/server/main.go
├── internal/
│   ├── api/           # HTTP 层
│   ├── http/          # HTTP 中间件（鉴权、onboarding bypass 等）
│   ├── service/       # 业务逻辑
│   ├── repository/    # 数据访问
│   ├── domain/        # 领域模型 + 仓储接口（依赖倒置，零外部依赖）
│   ├── worker/        # 异步任务 Worker（tasks 消费）
│   ├── maintenance/   # 每日维护调度
│   ├── storage/       # 文件落盘
│   ├── backup/        # 备份
│   ├── health/        # 体检
│   ├── seed/          # 预置数据导入
│   └── config/
├── migrations/        # schema 迁移
├── frontend/          # Vue 3 项目
├── docs/
│   ├── adr/           # 架构决策记录
│   └── api/           # API 文档
└── testdata/
```

**依赖方向（轻量 Clean Architecture）**：
- `api → service → domain ← repository`。**禁止反向**。
- `domain` 层**不 import 任何外部库**（无 `database/sql` / `echo` / `zap`），仅标准库；纯结构体、常量、领域错误、**仓储接口定义**。
- **接口定义在 `domain`，实现在 `repository`**（依赖倒置）：`service` 只依赖 `domain` 的接口，不 import `repository`。
- `repository` 是唯一 import `database/sql` 的地方。
- `cmd/server/main.go` 负责组装与依赖注入（`db → repository → service → handler → 路由`）。
- 不做：UseCase 类、Presenter、Mapper 层、Event Bus、CQRS（Paperly 规模下属过度设计）。

### domain 层规范（轻量 Clean · 路 1，见 ADR 0002 补丁 v2）

**必须**：
- 不 import 任何外部库（`sql`/`echo`/`zap`/`json` 皆禁止）
- 定义仓储接口（`XxxRepository`）
- 定义领域错误（`ErrNotFound`/`ErrInvalidInput`/…）
- 定义领域常量与枚举

**允许**：
- `db` tag（sqlx 扫描映射，无语义风险）
- `validate` tag（如需）

**禁止**：
- `json` tag（防序列化泄漏，见 ADR 0002）
- SQL 语句
- HTTP 相关类型（`echo.Context` 等）
- 日志调用

**验证**：
```bash
grep -rE 'json:"' internal/domain/       # 应 0 命中
grep -r 'database/sql' internal/domain/  # 应 0 命中
grep -r 'echo\.' internal/domain/        # 应 0 命中
```

---

## 枚举规范

- **存储值用整数**：DB 状态/类型列优先用 `INTEGER` 枚举（如 `kids.status` 1/2/3、
  `kids.gender` 1 男 0 女）。枚举常量定义在 `domain`（`KidStatusActive` 等），
  **代码禁止裸数字**，一律引用常量。历史英文枚举（`"active"`）已废止。
- **显示值走映射**：中文只在 DTO 层或前端映射（`frontend/src/lib/*`），不落 DB
- **验证**：
  ```bash
  grep -rn '"[一-龥]"' internal/repository/ --include='*.go'   # 应为 0（注释除外）
  ```

## 编码规范

### Go

- 所有导出函数第一个参数 `context.Context`
- 错误必须 `fmt.Errorf("...: %w", err)` 包装
- 禁止业务错误用 `panic`（仅 `main` 初始化失败可用）。契约型 `panic`（如 `WithTx` 类型错）不算业务 `panic`，允许。
- 禁止全局变量存状态（配置除外）
- 禁止 `init()` 做重活
- IO 密集场景（文件上传等）优先异步实现

### Vue / TypeScript

- 所有 `.vue` 用 `<script setup lang="ts">`
- Props 用 `defineProps<Interface>()`，Emits 用 `defineEmits<{...}>()`
- 禁止选项式 API
- 禁止组件内直接 `fetch`（必须走 `src/api/`）
- 禁止 `v-html` 渲染未净化内容
- **禁止 `any` 和 `as any`**；公共接口显式声明类型

### 通用

- 关键业务逻辑、复杂决策、异常路径必须有**必要注释 + 结构化日志**
- 代码达到可读性阈值后拆分模块（单文件建议 ≤ 300 行）
- 默认不保证向后兼容；破坏旧数据格式时**记录迁移边界和影响**

### 命名

| 场景 | 约定 |
|------|------|
| Go 文件 | `snake_case.go` |
| Go 接口 | 单方法用 `-er` 后缀 |
| DB 表 / 列 | `snake_case` |
| API 路径 | `/api/kebab-case` |
| Vue 组件 | `PascalCase.vue` |
| Pinia store | `useXxxStore` |

**数据库表名对齐（强制）**：代码中的**类 / 方法 / 对象 / 服务 / DTO / handler / 路由 / 磁盘路径**必须与数据库表名 1:1 对齐，**不得自创前缀或后缀**。

- 表 `exams`（试卷）→ `domain.Exam`、`ExamRepoSQLite`、`ExamService`、`ExamHandler`、`dto.ExamSummary/ExamDetail`、`ExamListResponse`、路由 `/api/exams`、存储目录 `assets/exams`。
- 表 `pages`（页）→ `domain.Page`（**不是** `ExamPage`）、`PageRepoSQLite`、`dto.PageSummary`、路由 `/api/pages`。
- 唯一例外：**UI 显示文案**可用自然语言（如「试卷」「页」），不受表名约束。
- 系统型 handler 豁免：聚合/系统能力（`Admin`/`Metrics`/`Dashboard`/`Onboarding`/`ImageSettings`/`KidInvite`/`Auth`）无单一归属表，不受 1:1 约束；实体型（`Exam`/`Kid`/`Subject`/`Page`/`Task`/`Message`/`User`/`KnowledgePoint`）必须遵守。
- 新增实体时先看 `DATABASE_DESIGN.md` 的表名，再据此命名代码，禁止反过来。

---

## UI 风格规范

**视觉权威**：**shadcn/ui 设计系统**（**new-york** 风格 + **neutral/zinc** 基色）。
组件层为手搓的 shadcn 风格组件（`package.json` 无 `shadcn-vue` 包），源码入库于 `frontend/src/components/ui/`，可自由修改；复杂交互复用 **radix-vue** 无头原语。
类名合并统一用 `cn()`（`clsx` + `tailwind-merge`），**禁止手写字符串拼接类名**。

**设计语言**：暗色优先 + **实色（不透明）卡片** + 细边框 + 极简灰阶 + 径向圆点背景。

### UI/UX Aesthetic & Styling Guardrails（强制）

#### 1. 颜色与层级（禁止纯黑 / 高饱和）

- **基色**：严格遵循 **Zinc/Neutral** 灰阶。**禁止**纯黑 `#000000`，**禁止**高饱和原色（如 `#3b82f6`、`bg-blue-500`）。
  - **例外 —— 功能性焦点色**：控件获得焦点时的描边（`focus-visible`）统一为 **2px 蓝色** `ring-2` + `ring-ring`（满不透明），明暗主题同色相；`--ring` 浅 `oklch(0.55 0.18 255)` `#026fd7` / 深 `oklch(0.72 0.16 255)` `#59a6ff`。此为可访问性焦点提示，非品牌强调色，不受上条「禁止高饱和原色」约束。
- **暗色背景**：
  - 画布底：`bg-background`（zinc-950 `#09090b`）
  - 抬升卡片：`bg-card`（zinc-900 `#18181b` + 细 `border-zinc-800`）
  - 下拉 / 弹层 / 对话框：`bg-popover`（**不透明**）+ `shadow-lg`
- **控件必须不透明**：按钮 / 输入 / 下拉 / 弹层等交互控件一律实色底（`bg-card` / `bg-background` / `bg-popover`），**禁止** `bg-*/50`、`bg-*/60`、`backdrop-blur-*`——页面带径向圆点背景，半透明控件会透出底纹，显脏。卡片亦统一实色，不再用毛玻璃。
  - **例外（非交互元信息，仍禁交互控件半透明）**：
    - **图例 chip**（`bg-accent/60` + `sm:bg-transparent`，仅移动端给底、桌面透明）——纯展示性图例，非可点击控件；
    - **状态块底**（如 `ErrorState` 的 `bg-destructive/10`、代码块的 `bg-muted/40`）——只读信息底；
    - **拖拽中反馈**（`bg-primary/5`）——瞬时状态提示。
- **结构边框**：用 `border border-border/50` 或 `border-zinc-800/80`，**禁止**重实线黑色描边。

#### 2. 间距、几何与节奏

- **卡片外壳**：统一 `rounded-xl border border-border/50 bg-card shadow-sm`。
- **内边距**：标准卡内 `p-6`（`p-4` 仅用于紧凑移动端或卡片内微卡）。
- **网格间距**：所有行的卡片之间统一 `gap-6`。
- **表单字段间距**：flex 容器内用 `space-y-4` 或 `gap-3`。
- **禁止**任意像素魔法值，如 `p-[14px]`、`m-[7px]`。

#### 3. 排版与信息层级

- **视觉降噪**：辅助元数据（时间戳、页码、状态标签、存储路径）必须 `text-xs text-muted-foreground font-normal`。
- **关键指标数字**：`text-2xl font-bold tracking-tight font-mono` 或 `font-sans`。
- **标题**：
  - 卡片标题：`text-base font-semibold tracking-tight text-foreground`
  - 副标题 / 描述：`text-xs text-muted-foreground mt-1`
- **可扫读**：正文用 `leading-relaxed`；数据标签用 `tabular-nums`。

#### 4. 微交互与状态打磨

- **过渡基线**：所有可交互元素必须声明 `transition-all duration-150 ease-out`。
- **按钮**：
  - 主按钮：`bg-primary text-primary-foreground shadow-sm hover:bg-primary/90 active:scale-[0.98]`
  - 次按钮 / Ghost：`hover:bg-accent hover:text-accent-foreground active:scale-[0.98]`
  - **触控目标**：移动端可点击区最小 44×44px（`size-11`，`sm:` 起可收至 36px）；纯桌面弹窗内控件最小 28px，移动端可用时同样升至 44px。
- **数据表行**：可点击行用 `hover:bg-muted/30 transition-colors cursor-pointer`。
- **加载与空态**：
  - 加载：骨架脉冲 `animate-pulse bg-muted rounded-md`，**形态必须贴合最终内容**。
  - 空态：居中细图标（`opacity-40`）+ `sm` 标题 + muted 操作提示；**禁止**留空白容器。
  - 图标统一走本仓 `Icon.vue`（Material Symbols Rounded 自托管子集），**不引入 lucide-vue-next**（见「图标规范」）。

### 设计 token（`frontend/src/theme/tokens.css`）

| Token | Light | Dark | 用途 |
|-------|-------|------|------|
| `background` | `#ffffff` | `#09090b` | 页面背景（zinc-950） |
| `card` | `#ffffff` | `#18181b` | 卡片背景（zinc-900） |
| `foreground` | `#0a0a0a` | `#fafafa` | 主文字 |
| `muted-foreground` | `#737373` | `#a1a1a1` | 次/禁用文字 |
| `primary` | `#171717` | `#e5e5e5` | 主色（**neutral**，不用蓝） |
| `destructive` | `#e7000b` | `#ff6467` | 危险/错误 |
| `success`/`warning`/`info` | chart-2/5/1 | chart-2/3/1 | 语义色（除 destructive 外取自 chart 调色板） |
| `border` / `input` | `#e5e5e5` | 白 10%/15% | 边框 |
| `chart-1..5` | 蓝/绿/琥珀/紫/红（= 暗色同色相，亮度按白底微调） | 蓝/绿/琥珀/紫/红 | 图表调色板（**两主题色相严格一致**：换主题时线条颜色系不变，只调明度） |

- **圆角**：`--radius: 10px`；派生 sm 6 / md 8 / lg 10 / xl 14
- **字体**：`--font-sans` = Geist；`--font-title` = Jersey 15（仅 LOGO）
- **主题机制**：`:root` = light，`:root.dark` = dark；`theme/mode.ts` 控制（默认 **dark**）
- **阴影**：极轻（卡片 `shadow-sm`、按钮 `shadow-xs`、浮层 `shadow-md`）

### 基础组件（`frontend/src/components/`）

| 组件 | 骨架要点 |
|------|----------|
| `KpiCard` | `bg-card` + `border` + `rounded-xl` + `shadow-sm` |
| `Button` | 六变体 default/destructive/outline/secondary/ghost/link；四尺寸 default h-9/sm h-8/lg h-10/icon size-9；强制 `transition-all duration-150 ease-out active:scale-[0.98]` |
| `StatusBadge` | `rounded-md px-2 py-0.5 text-xs font-medium`；变体 default/secondary/destructive/outline/success/warning |
| `Table` | 表头 `text-muted-foreground text-sm`、极细边线、空值 `—`、slot 单元格；可点击行 `hover:bg-muted/30 transition-colors cursor-pointer` |
| `ChartWrapper` | vue-chartjs（`Line`）卡片；`theme/chart.ts` 主题随 mode 重算；默认高 320px |
| `Layout` | 主区 `max-w-5xl`、header 左 LOGO 右导航、无 sidebar |
| `Logo` | Jersey 15 + `> Paperly`（前缀 `> `，无 `+`） |
| `BackgroundPattern` | 径向圆点网格 + 中心径向遮罩 |
| `Icon` | Material Symbols Rounded（自托管子集，PUA 寻址）——**全站唯一图标来源** |

### 图标规范

- **统一 Material Symbols Rounded**，禁止混入 lucide 或其他图标库
  （上文空态规则提到的 `lucide-vue-next` 仅为通用示例；本仓**不引入**，空态图标一律走 `Icon.vue`）
- 字体子集化后自托管：`frontend/src/assets/fonts/material-symbols-rounded.woff2`（~4.7KB）
- 通过 `Icon.vue` 的 `name` prop 使用；名称→码位映射见该文件 `CODEPOINT`
- **常用图标清单**（新增图标须同步子集字体 + `CODEPOINT` + 本清单）：
  - 导航：`description` / `people` / `settings`
  - 操作：`add` `edit` `delete` `save` `cancel` `search` `filter` `refresh` `download` `upload`
  - 状态：`check_circle` `error` `warning` `info` `pending`
  - 文件：`image` `picture_as_pdf` `folder`
  - 用户：`person` `person_add` `visibility` `visibility_off`
  - 图表：`bar_chart` `show_chart` `pie_chart`
  - 主题：`light_mode` `dark_mode` `brightness_auto`
  - 通用：`menu` `close` `arrow_back` `arrow_forward` `chevron_left` `chevron_right` `home` `logout` `calendar_today` `trending_up` `trending_down` `star` `bookmark` `notifications` `help` `open_in_new` `more_vert` `check` `clear` `expand_more` `expand_less` `expand` `restore_from_trash` `restore` `settings_backup_restore` `horizontal_rule`

### Console banner 规范

- 框式 ASCII art（`_/_/...` 上下边框），**无 ANSI 颜色**
- 经**结构化日志**输出（`logger.Info`），与访问日志同格式
- 含版本行（`Version` / `Build time`）
- **每次启动**打印（非仅 dev）
- Paperly 实现：`cmd/server/banner.go`，在 `server started` 日志前调用

---

## SQLite 使用规约

1. **参数化查询**，禁止字符串拼接 SQL
2. **大字段落文件系统**（图片等大文件），DB 只存路径
3. **写串行化走写池**（单连接 + `_txlock=immediate` + `busy_timeout`），禁止绕过写池直接 `db.Exec`
4. **禁止 N+1 查询**，必须批量化
5. **禁止 `SELECT *`**，显式列出字段
6. **禁止无 `WHERE` 的 `UPDATE` / `DELETE`**
7. **读写池分离**：写 `Write()`（`SetMaxOpenConns(1)` + `_txlock=immediate` + `busy_timeout=5000`），读 `Read()`（WAL 多连接）——依据 ADR 0001 / PoC 6
8. **备份用 `VACUUM INTO`**（在线不停机），不使用 sfsDb 的 `Flush`/`AcquireBackupLock`
9. **Row Scan 用 sqlx**：`db.GetContext` / `db.SelectContext` 替代手写 Scan
10. **SQL 全部手写**：不引入 ORM；sqlx 仅做映射层
11. **无 PO 层**：`domain` 实体直接做扫描目标（带 `db` tag）

### Schema 迁移

- **首版**：单脚本 `0001_init.sql`（所有 DDL `IF NOT EXISTS` + `INSERT OR IGNORE` 幂等），整个脚本在**一个事务**内执行，失败整体回滚；迁移后写 `migrations(version, name, display_tag)`。
- **未来**：`ALTER / ADD COLUMN` 无法靠 `IF NOT EXISTS` 幂等 → **换正式机制**：`NNNN_name.sql`（由 `migrations` 表按 version 判重）+ 迁移前**暂停写入 + `VACUUM INTO` 完整备份**（在线），迁移在事务内、失败回滚。

### 事务边界（三链）

- 三链写路径**默认单事务原子提交**：如 exam 上传（exam 行 + page 行）、删页重排（delete + renumber + task 入队）均封装为 `WithinTx`。
- **例外：`RebuildPages`（题库重建）**——占位页 + 计数可持久化；整链回滚归**数据校验**（先验后写）：重建前校验源数据一致性，失败不污染主链，不依赖事务回滚兜底。

---

## API 约定

### 路由

```
GET    /api/kids
POST   /api/kids
GET    /api/kids/:id
PUT    /api/kids/:id
DELETE /api/kids/:id
POST   /api/exams/upload
GET    /api/exams
GET    /api/pages/:id/file
```

### 列表分页（keyset 游标，见 ADR 0003）

- 请求 `GET /api/exams?cursor=<id>&limit=20`；`cursor` 省略/0 = 从最新开始。
- 响应 `{ items, next_cursor, has_more }`；`next_cursor=0` ⟺ `has_more=false`。
- **不返回 `total`**（游标分页不提供精确总数）。
- 排序键必须是游标列（`id DESC`）；SQL `WHERE id < :cursor ORDER BY id DESC LIMIT :limit+1`（多取 1 探测 has_more）。
- 不用 `LIMIT/OFFSET`：offset 在边翻边删/增时漏行或重复。

### 响应格式

```json
{ "code": 0, "data": { ... } }
{ "code": 40004, "message": "exam not found", "detail": "exam_id=123" }
```

### 中间件顺序

`Recover → Logger → CORS → JWT → Validator → Handler`

### DTO 规范（见 ADR 0002）

**三视角原则**：每个实体的 API 契约按视角分，**不复用同一 struct**。

| 视角 | 用途 | 必含字段 |
|------|------|----------|
| **Summary** | 列表 / 引用 | `id` + **业务标识** + `status`（若该表有 status 列） |
| **Detail** | 详情 / 编辑 | Summary 全字段 + 详细字段 |
| **Created** | 创建响应 | `id` + 一次性字段（如 `access_code`）+ 关联 URL |

**业务标识（按实体）**：

| 实体（表） | 业务标识 |
|------|----------|
| Kid（`kids`） | `name` |
| Exam（`exams`） | `title` |
| Page（`pages`） | `page_index` |

**硬约束**：
- **三态实体的 Summary 必含 `status`**——`ExamSummary` 等有 `status` 列的实体必须带
- **Created 视角只暴露一次性字段**——`access_code` 只在创建响应出现
- **Detail 不含内部路径**——`file_path` 等内部路径只在服务端用，不出 DTO
- **禁止 `json` tag 出现在 domain**（ADR 0002）

### 预留契约（首版无消费者，勿删）

下列响应字段目前**没有任何前端消费方**，但均有明确用途，属 REST/领域约定而非死代码，**不得当作未使用代码删除**；如日后移除须先同步最终用户（家长/孩子）视角的语义：

| 字段 | 所在接口 | 保留理由 |
|------|----------|----------|
| `TaskListResponse.counts` | `GET /api/tasks` | 任务清单的"待复习/进行中/已完成"计数，列表页语义预览 |
| 登录 Response 的 `user` / `kid` | `POST /api/auth/*login` / `onboarding/complete` | REST 风格：登录返回主体信息而非仅 token，供后续免二次拉取 |
| `KidCreateResponse` 的 `kid_url` / `resetAccessCode` | 创建/重置访问码 | 一次性凭证反馈（与 `access_code` 同为 Created 视角一次性字段） |
| `ExamUploadResponse.id` / 上传多页 | 上传接口 | 上传即返回主键，避免校正其后再补一次查询 |

领域枚举预留（常量/分支/SQL CASE 已落位，仅尚无生产者触发，**勿删**）：`MessageKind` 5-8 / `UserStatusDisabled` / `SummarySource*`。

---

## AI / OCR 适配器

> **已移除**：首版未上线，AI / OCR 适配器（原 `internal/ai/`）、OCR / 切分服务、相关配置与 DTO 已从代码库删除。
> 如日后重新引入，按「常见陷阱」的接口隔离原则从零设计，勿依赖历史实现。

---

## 测试

| 层 | 覆盖率 |
|----|--------|
| domain | ≥ 80% |
| repository | 关键路径 100% |
| service | 分支覆盖 |
| api | 关键接口 100% |

### 必测边界

- 列表 `cursor=0`/缺省从最新开始；`next_cursor=0` ⟺ 无更多
- keyset 翻页中删除已翻过的行不漏行（对比 OFFSET 的失效场景）
- `exam_date` 闭区间两端都含；逆序端点自动交换
- 详情 `page_count` 等于 `len(pages)`（`GetByID` 不跑子查询）
- `ScopeCounts` 的 `this_month`/`last_month` 用 `tsLayout` 文本区间比较 `created_at`（写侧 CURRENT_TIMESTAMP 即该格式）；`dateLayout`（DATE 列）由 `scanSeries` 用 `time.Parse` 解析，脏行跳过（`TestScopeCountsKid`）
- `genTitle` 缺任一必需项返回空串，不生成残标题

---

## Git 提交

```
<type>(<scope>): <subject>

[body]

[footer]
```

**type**：`feat` / `fix` / `refactor` / `perf` / `test` / `docs` / `chore` / `poc`
**scope**：模块名，如 `exams`、`kids`、`dashboard`、`api`

**禁止**：
- 一个 commit 干多件事
- `git commit -m "update"`
- 提交未通过的测试
- 提交含调试代码 / `fmt.Println`

---

## 领域建模与决策记录

**触发场景**：领域概念、边界、状态模型或重要架构决策变化时；新增表 / 改字段语义 / 修改算法判定阈值 / 调整 API 契约。

**动作**：
- 写 `docs/adr/NNNN-title.md`（Architecture Decision Record）
- 领域模型以 `DATABASE_DESIGN.md` 的 16 张表为唯一权威源：
  `users` / `kids` / `exams` / `pages` / `subjects` / `kid_subjects` /
  `knowledge_points` / `migrations` / `metrics` / `cursors` / `tasks` /
  `questions` / `question_kps` / `levels` / `gaps` / `messages`
  （其中 `questions` / `question_kps` / `levels` / `gaps` 为预留未接线，见 `DATABASE_DESIGN.md` 标注）

---

## 任务执行流程

### 1. 任务理解

- 确认对应 `REQUIREMENTS.md` 章节编号
- 与需求冲突 → **先问人类，不擅自改**
- 需要新表 / 新字段 → **先确认 DATABASE_DESIGN.md 已定义**

### 2. 方案设计

- 多文件改动 → 先输出方案（≤ 200 字）
- 算法改动 → 明确输入输出 + 边界 + 测试用例
- Schema 改动 → 先写迁移脚本

### 3. 编码

- 按上述规范
- 写测试
- 组织 commit

### 4. 自检（必做）

- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `golangci-lint run` 无错误
- [ ] `bun run build` 通过
- [ ] `bun run test 2>&1 | grep '✗' || echo "NO-FAILURES"` 返回 NO-FAILURES
- [ ] 关键路径有测试
- [ ] 无 `TODO` / `FIXME` 遗留
- [ ] 无调试代码
- [ ] 无 `any` / `as any`
- [ ] commit message 符合规范

### 5. 交付

- 输出：改动清单 + 测试结果 + 关联需求编号
- 引入 ADR → 写入 `docs/adr/`
- 发现需求硬 bug（编码即错）→ **先报告，不擅自改需求**

---

## 红线（违反即打回）

| # | 禁止 | 理由 |
|---|------|------|
| F1 | 换技术栈 | `TECH_STACK.md` 已冻结 |
| F2 | 改需求文档 | 已冻结；硬 bug 先报告 |
| F3 | 引入 CGO 依赖 | 破坏跨平台单二进制 |
| F4 | 引入多租户 / `tenant_id` | 无多租户 |
| F5 | 引入 Redis / MQ / ORM | 与极简目标冲突 |
| F6 | 绕过写池直接 `db.Exec` | 破坏写池串行模型 |
| F7 | 跨链操作 | 破坏三链事务边界 |
| F8 | AI 产出直接落业务表 | 违反 REQUIREMENTS.md 审核机制 |
| F9 | 前端裸 `fetch` | 违反 API 封装约定 |
| F10 | 提交调试代码 / 硬编码密钥 | 安全底线 |
| F11 | `panic` 用于业务错误（契约型 `panic`（如 `WithTx` 类型错）除外） | 破坏服务稳定性 |
| F12 | 无测试的算法 / 关键逻辑改动 | 行为变更须有测试守住 |
| F13 | TypeScript 使用 `any` / `as any` | 破坏类型安全 |
| F14 | 凭记忆调用外部 API | 版本漂移必踩坑 |

---

## 常见陷阱

| 陷阱 | 正确做法 |
|------|----------|
| 循环里逐条查询 | 批量化（一次查询用条件聚合 / `IN (...)` 取回，别在循环里打 SQL） |
| 空输入不设防 | 函数入口 `if len(x) == 0 { return ... }` |
| `COALESCE` 对空结果集无效 | 外层包 `COALESCE((SELECT ...), 'default')` |
| 计算函数内部 save | 计算与持久化分离 |
| 忘记 `rowsByKP[kp]` 为 nil 的告警 | 显式 warn + 跳过，**不清零** |
| SQL 字符串拼接 | 参数化查询 |
| 前端 `any` 泛滥 | 显式类型定义 |
| 提交前不跑 `go test` | 每次 commit 前必跑 |
| 凭记忆用旧版 Echo / Vue / Tailwind API | 查官方文档确认当前版本 |
| `PreparexContext` 用于 `RETURNING` + 命名参数报 `unsupported type domain.XXX` | 命名参数用 `PrepareNamedContext`（`PreparexContext` 只支持位置参数） |
| Path/归属参数未在 service 签名显式接收，漏设导致外键=0 | path 参数（`exam_id`/`kid_id` 等）必须作为 service 方法入参显式传递，禁止依赖 request body 或隐式上下文 |
| "上传者"与"归属"共用一个字段 | 两者是不同事实：`exams.uploaded_by`（谁传的，审计，只读）/ `exams.kid_id`（归谁，业务，可改）。共用一个字段会导致"家长代传的卷子孩子看不到"（已拆分） |
| SPA catch-all（`/*`）先于 API 路由注册 | `/*` **最后**注册，否则吞掉 `/api/*` 与 `/health`（Echo 静态 handler 亦同） |
| `go:embed` 目标目录未构建时缺文件 | 目录内提交占位文件（`.gitkeep` + placeholder 页），否则首次 clone 未 `bun build` 时 `go build` 失败 |
| 以为 Bun 原生编译 `.vue` | **否**：`bun build` 把 SFC 当静态资源原样拷贝，产物是 `.vue` 源码；须自写 plugin 调 `@vue/compiler-sfc`（`bun-plugin-vue@1.0.0` 是空壳） |
| `bun build` CLI 传 plugin | CLI **不支持** `plugins`，必须走 `Bun.build({ plugins })` API |
| 构建后不验证运行时 | 产物含 render 函数 ≠ 运行时正确；用 happy-dom 挂载实测（本坑靠 `in` operator 报错才发现） |
| handler 参数用 `echo.Context`（v4 接口） | Echo v5 handler 参数是 `*echo.Context`（指针，非接口）；middleware/测试构造同改 |
| 用 `e.Start()` / `e.Shutdown()` / `e.HideBanner` | v5 无这些方法；启停用 `echo.StartConfig{...}.Start(ctx, e)`（内建信号优雅关闭） |
| 用 `echozap` 桥接日志 | 仅支持 v4；v5 日志走 `slog`，若需 zap 则自写 `slog.Handler` 桥（`cmd/server/logging`） |
| 用第三方 UI 库的 `useDialog`/`useMessage` 等 provider 依赖组件 | 页面级组件测试直接挂载页面、不挂 `App.vue`，provider 注入失败；用仓内 `ConfirmDialog`/`Modal`（自包含），勿依赖 provider |
| `/api/*` 的 404 在 `/*` 之后注册 | 必须在 `/*` 之前注册，否则落 SPA fallback 返 200 HTML 静默吞错 |
| 组件用了需要外层 provider 的第三方 composable（如 `useDialog`/`useMessage`） | 页面级组件测试直接挂载页面、不挂 `App.vue`，provider 注入失败；交互组件用**仓内自包含**实现（`components/ui/`），不依赖 provider |
| Chart.js v4 用具名导出的控制器做 `Chart.register` | **会抛 `"bar" is not a registered controller`**——拆包后控制器模块只含类型、不含类实例；必须 `import "chart.js/auto"`（代价：无法 tree-shake）。构建期与 typecheck 均不报错，须运行时冒烟 |
| 手写字符串拼接 Tailwind 类名 | 用 `cn()`（`clsx` + `tailwind-merge`）；冲突不报错，只会静默产生错误样式 |
| 以为 shadcn-vue 组件能 `bun update` 升级 | 组件是**源码入库**的——升级靠重新生成/手改，不是包更新 |
| 对 shadcn-vue 组件用 `v-model:value`（旧 UI 库习惯） | 统一 `v-model`；Dialog 是受控组件，用 `v-model:open` 控制显隐 |
| `Calendar`/`DatePicker` 把日期当 `Date` 用 | 本轮日期维持原生 `input[type=date]`，避免 `CalendarDate` 类型转换 |
| 前端页面按 v1.0 契约发请求（`grade` 传字符串、提交已不存在的列） | 后端契约以 `internal/api/dto` 为准；改 schema 后同步前端类型与表单，别只改一边 |
| 分页用 `LIMIT/OFFSET` | 列表用 keyset 游标（`id < cursor`），offset 在增删下会漏行/重复（ADR 0003） |
| 手改后仍被自动生成覆盖（或自动值锁死手改） | 自动生成（如 `genTitle`）只在「用户未手改」时写入；用显式 `@input` 标记手改，别用值 watcher（会被自动写入的空串误判） |
| 详情 DTO 的 `page_count` 直接透传 `domain.Exam.PageCount` | `GetByID` 不跑页数子查询，该字段恒为 0；详情用 `len(pages)` 回填 |

---

## 品牌隔离

**Paperly 仓库中不得出现任何外部项目的标识**（项目名、logo/品牌元素、URL/路径、作者/组织、版权声明）。具体：

- 代码、注释、文档、测试、字符串、资源文件名、变量名、API 路径、UI 文案、commit message，一律不得携带外部项目痕迹。
- 禁止任何"指明外部来源"的表述（如"取自某项目""模仿某实现"），Paperly 模块之间的引用除外。
- 若需说明来源，只能引用**通用设计系统/库**（如 shadcn/ui、Tailwind、Radix、Vue）。
- 视觉值（颜色/圆角/间距）即 Paperly 自己的设计选择，**不注明来源**。
- 验收：`grep -ri` 外部项目名 + `git log --all --format=%B` 均零命中。

---

## 参考文档

- `DATABASE_DESIGN.md` — **16 张表的唯一权威源**（表名 / 字段 / 枚举）
- `TECH_STACK.md` — 技术栈选型与理由
- `REQUIREMENTS.md` — 权威源
- `docs/adr/` — 架构决策记录
- `docs/api/` — API 文档

---

## 变更记录

| 日期 | 变更 |
|------|------|

---

> **核心原则**：能跑、能测、能提交的代码，比完美的设计文档更有价值。
>
> **不做**：不改需求、不换栈、不引入新技术、不做无测试的算法改动、不用 `any`、不凭记忆调 API。
>
> **必做**：显式错误、参数化查询、写队列、批量 save、自检清单全过。
