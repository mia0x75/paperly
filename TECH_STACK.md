# Paperly 技术栈

> **版本**：v1.0.0
> **最后更新**：2026-10-13
> **权威性**：本文档是技术选型 / 依赖 / 构建 / 部署的权威源。
> **去重原则**：数据库 schema 见 `docs/DATABASE_DESIGN.md`；产品需求见 `docs/REQUIREMENTS.md`；API 见 `docs/api/API.md`；前端路由见 `frontend/src/router/ROUTES.md`。

## 一、后端

| 层 | 选型 | 版本 | 说明 |
|----|------|------|------|
| 语言 | Go | 1.26+ | `go.mod` 指令；标准库 `embed` 用于前端嵌入 |
| Web 框架 | **Echo** | v5 | 性能第一梯队（零分配）、100% `net/http` 兼容、生态成熟 |
| 数据库 | **modernc.org/sqlite** | latest | 纯 Go、无 CGO、单文件 SQLite；WAL 模式多读单写；应用层串行写队列（见 `docs/adr/0001`） |
| 数据访问 | **database/sql + jmoiron/sqlx** | latest | sqlx 只做 scan 简化（GetContext/SelectContext/NamedExec/In）；SQL 全部手写；不用 ORM |
| 认证 | `golang-jwt/jwt` | v5 | 家长（`typ=parent`）+ 孩子（`typ=kid`）双 token；中间件按 `typ` 分派 |
| 图像处理 | `KarpelesLab/gowebp` + `disintegration/imaging` | latest | WebP 编码（上传页图/题图转码）+ 缩放裁剪；纯 Go，无 CGO |
| 日志 | `go.uber.org/zap` | latest | 结构化日志，供 `metrics` 与运维使用 |
| 测试 | `stretchr/testify` | latest | assert / require / mock / suite；表格驱动 |
| 热重载 | `air-verse/air` | latest | 开发期自动编译重启（`go install` 工具，未进 `go.mod`） |

---

## 二、前端

| 层 | 选型 | 版本 | 说明 |
|----|------|------|------|
| 框架 | **Vue** | 3.5+ | 组合式 API（`<script setup>`）；仪表盘型 SPA 最佳匹配 |
| 状态管理 | **Pinia** | 3 | Vue 官方推荐；TypeScript 类型安全 |
| 路由 | **Vue Router** | 4 | history 模式，配合 Go SPA fallback |
| 图表 | **vue-chartjs** + **Chart.js** | 5.x + 4.x | vue-chartjs v5 配 Chart.js v4（peer `chart.js ^4.1.1`）；组件化封装、chart.js 本体 min 约 200KB（ECharts 达 54MB）、`chart.js/auto` 全量注册免手工 `use()`。**注**：弱点雷达暂无后端 `levels` 数据，`DashboardPanels` 用假数据灰色占位渲染（非注释留空），待 `levels` 落地后替换为真数。 |
| 样式 | **Tailwind CSS** | 4 | 现代浏览器基线：Chrome 111+ / Safari 16.4+ / Firefox 128+ |
| UI 组件 | **radix-vue 原语 + 手搓 shadcn 风格组件** | radix-vue ^1.9 | `package.json` 无 `shadcn-vue`：`components/ui/` 为手写源码（cva + `cn()`），部分复用 radix-vue 无头原语（Calendar）；基于 Tailwind 4，与设计 token 同源（见 `docs/adr/0010`） |
| 无头逻辑 | **radix-vue** | ^1.9 | 日历等复杂交互的无头行为；DropdownMenu / Select 为全手搓实现，不依赖 radix |
| 富文本 | **Tiptap** | 3.x | `@tiptap/vue-3` + `@tiptap/starter-kit`（含底层 `@tiptap/pm`）；`SummaryEditor` 的追加式总结编辑器，输出受限 HTML 供 `lib/summary.ts` 打 `data-sig` 署名段。 |
| HTTP | **fetch**（原生） | — | 替代 `ofetch`；零依赖，统一封装在 `api/client.ts`（保持 `apiGet/apiPost/apiPut/apiDelete/apiUpload` 签名不变）。 |
| 语言 | TypeScript | 5.x | 前端全量 TS |

---

## 三、构建与交付

| 层 | 选型 | 说明 |
|----|------|------|
| 前端构建 | **Bun + 自写 Vue 插件**（不引 Vite） | `Bun.build({ plugins: [vuePlugin] })`，插件调官方 `@vue/compiler-sfc`；Bun **不原生编译 `.vue`**（会当静态资源拷贝）；配套 Tailwind CLI 编译 CSS |
| 产物形态 | `index.html` + 哈希命名 JS/CSS | Bun 按需产出哈希资源文件（非单 HTML 内联）；`--public-path=/` 保证绝对路径 |
| Go 嵌入 | `//go:embed all:dist` | 前端产物嵌入 Go 二进制；目录内提交占位文件，未构建也可编译 |
| SPA Fallback | 自定义 handler + `http.FileServer` | 真实文件直接返回；否则 fallback `index.html`；`/*` **最后注册** |
| 开发模式 | Bun dev server + Go `air` | `dev-proxy.ts` 同源反向代理 `/api` → `:8080`，零 CORS |
| 生产模式 | `make build`（frontend → go build） | 单二进制交付，含全部前后端代码 |

> **构建坑注脚**：
> - **`--public-path=/` 必须加**：`bun build` 默认输出**相对**资源路径（`./index-xxx.js`）。SPA 深链下（如 `/students/1`）浏览器将相对路径解析为 `/students/1/index-xxx.js` → 404。`--public-path=/` 强制绝对路径。
> - **`/*` SPA catch-all 必须最后注册**：否则会吞掉先注册的 `/api/*` 与 `/health`。
> - **`go:embed` 目标目录须有占位文件**：否则首次 clone 未 `bun run build` 时 `go build` 直接失败（`internal/web/dist/.gitkeep` + `internal/web/placeholder/`）。

### 交付流程

```bash
# 1. 一键构建（前端 → 嵌入 → 后端）
make build                            # → bin/paperly

# 或分步：
cd frontend && bun run build          # → internal/web/dist/{index.html,*.js,*.css}
go build -ldflags="-s -w" -o paperly ./cmd/server

# 2. 运行
./paperly                             # 单文件，监听 :8080
```

---

## 四、运维与部署

| 层 | 选型 | 说明 |
|----|------|------|
| 部署形态 | Docker + `docker-compose` | 一行命令启动 |
| 数据持久化 | `/data/paperly/` 挂载 | `paperly.db` + `assets/` + `seed/` + `backup/`（无 `ocr/`，OCR 产物落卷内 `questions/`） |
| 备份 | `VACUUM INTO` + 分级快照（daily 7 / weekly 4 / monthly 12） | 在线不停机原子快照；`REQUIREMENTS.md` 8.2 节口径 |
| 监控 | `metrics` 表 + `/health` 端点 | 体检页读近 7 天 counter / 当日 gauge（见 `DATABASE_DESIGN.md` `metrics` 节） |
| 日志 | 结构化 JSON 落盘 | 单机场景，不做 ELK / Loki 集成 |

### Docker 一键启动

```bash
docker run -d \
  -p 8080:8080 \
  -v /data/paperly:/data \
  paperly/box:latest
```

---

## 五、明确不引入的技术

| 技术 | 不引入理由 |
|------|-----------|
| **HTMX 4** | 与 Vue 3 范式冲突；NPM 尚未转正；Paperly 是仪表盘型 SPA，非超媒体应用 |
| **React** | Bundle 体积约为 Vue 的两倍；Bun 集成路径更长；学习曲线更陡 |
| **Redis** | 单机场景，无缓存层需求；应用层已有写队列 |
| **PostgreSQL / MySQL** | modernc.org/sqlite 单文件足够；Cloud 版再单独评估 |
| **ORM（GORM 等）** | SQLite 直接 `database/sql` 风格即可，无需重型 ORM |
| **消息队列（Kafka / RabbitMQ）** | 单机场景，应用层串行写队列已覆盖并发需求 |
| **Kubernetes** | Docker 单容器交付，K8s 过度设计 |
| **SSR / RSC** | Paperly 前端为纯 API 客户端，无 SSR / 流式渲染需求 |
| **viper / yaml.v3** | `config.json` 用标准库 `encoding/json` 足够；密钥只从 env 读，不落文件；引第三方配置库只会多一层「我改了怎么没生效」的支持成本 |

---

## 六、已知约束

| 约束 | 说明 | 应对 |
|------|------|------|
| Tailwind 4 浏览器基线 | Chrome 111+ / Safari 16.4+ / Firefox 128+ | 在 UI 增加浏览器检测提示；不支持旧浏览器 |
| 单写模型 | SQLite WAL 多读单写 | 应用层串行写队列；全量重算期间暂停录入 |
| 单文件大小 | ≥ 2GB 上限 | 大文件（试卷图）落文件系统，DB 只存路径 |

---

## 七、环境搭建

```bash
# 后端
go mod init paperly
go get github.com/labstack/echo/v5
go get github.com/jmoiron/sqlx
go get modernc.org/sqlite
go get github.com/golang-jwt/jwt/v5
go get go.uber.org/zap
go get github.com/stretchr/testify
go install github.com/air-verse/air@latest

# 前端
bun init -y
bun add vue@latest vue-router@4 pinia@3 vue-chartjs chart.js
bun add -d tailwindcss@4 typescript
```

**目标**：`go run ./cmd/server` 起 Echo + SQLite，`curl localhost:8080/health` 返回 `{"status":"ok"}`。

---

## 八、变更记录

| 日期 | 变更 |
|------|------|
| v1.0.0 | 基线（以各章节现状为准） |

---

> **Paperly** — Scan. Score. Succeed.
> Your data. Your server. Your rules.
```
