# Paperly 拾卷

面向中考备考的家庭**本地化试卷管理系统**：试卷上传归档、逐卷管理、首页统计看板、学科与知识点基础数据，全部数据留在自己的机器上。

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](./LICENSE)

## 特性

- **自托管单机**：一个二进制跑全家，无云端依赖、无多租户，数据不出家门
- **双端使用**：家长端（管理/分析）+ 孩子端（独立访问码登录，查看自己的试卷与统计、上传试卷）
- **试卷管理**：多页批量上传、上传即 WebP 压缩归档、软删除与恢复、按孩子/学科/类型/日期筛选
- **首页看板**：试卷 KPI、科目分布、历史试卷列表、存储增长趋势
- **基础数据**：学科、知识点、孩子档案、年级自动晋升
- **运维内建**：冷备份（日/周/月三级保留）、数据体检、后台任务队列（备份/清理/升年级/页面重排）
- **离线可用**：不依赖任何外部服务，断网也能正常使用

## 截图

### 首页看板

KPI 概览、上传坚持度趋势（支持 7 天 / 30 天切换、多孩对比）。

![首页看板](https://raw.githubusercontent.com/mia0x75/paperly/main/screenshots/screenshot-1-dashboard.png)

### 数据分布与试卷列表

按学科分布、掌握度雷达（示例数据）、最新试卷列表。

![数据分布与试卷列表](https://raw.githubusercontent.com/mia0x75/paperly/main/screenshots/screenshot-2-distribution.png)

### 系统运行监控

数据库完整性、Schema 版本、最近备份、文件系统、并发冲突、每日维护游标。

![系统运行监控](https://raw.githubusercontent.com/mia0x75/paperly/main/screenshots/screenshot-3-health.png)

## 技术栈

| 层 | 选型 |
|----|------|
| 后端 | Go 1.26+ · Echo v5 · SQLite（modernc.org/sqlite，纯 Go 无 CGO） |
| 前端 | Vue 3.5+（`<script setup>`） · Pinia 3 · Vue Router 4 · Tailwind CSS 4 |
| 图表 | vue-chartjs 5 + Chart.js 4 |
| 构建 | Bun（前端） · go:embed 单二进制交付（后端） |

> 权威选型见 [TECH_STACK.md](./TECH_STACK.md)。

## 快速开始

### Docker（推荐）

```bash
docker compose up -d
# 打开 http://localhost:8080
```

数据落在 named volume `paperly-data`（`paperly.db` + `assets/` + `seed/` + `backup/`）。

### 本机构建

```bash
make build     # 前端(Bun) + 后端 一并编译到 bin/paperly
./bin/paperly  # 默认监听 :8080，数据目录 <可执行文件目录>/data
```

### 开发

```bash
make run          # go run ./cmd/server（需已有 internal/web/dist 前端产物）
make dev          # air 热重载（需安装 air）
cd frontend && bun run dev   # 前端开发代理
```

### 常用命令

| 命令 | 说明 |
|------|------|
| `make build` | 编译前后端到 `bin/`（前端产物经 go:embed 打入单二进制） |
| `make test` | `go test ./...` |
| `make lint` / `make vet` | golangci-lint / go vet |
| `make fmt` | gofmt 全库格式化 |
| `make clean` | 清理 `bin/`（不动 `./data`） |
| `cd frontend && bun run test` | 前端测试 |
| `cd frontend && bun run typecheck` | vue-tsc 类型检查 |

## 配置

优先级：环境变量 > `config.json`（可执行文件同目录，可用 `PAPERLY_CONFIG` 指定）> 代码默认值。

| 配置 | env | 默认 | 说明 |
|------|-----|------|------|
| 数据目录 | `PAPERLY_DATA_DIR` | `<exe>/data` | 存放主库、附件、备份 |
| 监听地址 | `PAPERLY_ADDR` | `:8080` | HTTP 监听 |
| 图片压缩档位 | `PAPERLY_IMAGE_COMPRESS_PRESET` | `3` | 1~5，上传图片 WebP 压缩档 |

只有「配错了」才启动失败，「没配置」一律走默认——详见 `internal/config`。

## 目录结构

```
paperly/
├── cmd/server/main.go     # 入口：装配、路由、优雅退出
├── internal/
│   ├── api/               # HTTP 层（handler + DTO）
│   ├── service/           # 业务逻辑
│   ├── repository/        # SQLite 数据访问（写池串行）
│   ├── domain/            # 领域模型 + 仓储接口（零外部依赖）
│   ├── worker/            # 异步任务 Worker
│   ├── maintenance/       # 每日维护调度
│   ├── storage/ backup/ health/ seed/ ...
│   └── web/               # go:embed 前端产物挂载点
├── migrations/            # schema 迁移
├── frontend/              # Vue 3 项目
└── docs/                  # 需求 / 技术栈 / 库表设计 / API / ADR
```

## 文档

- [REQUIREMENTS.md](./docs/REQUIREMENTS.md) — 需求与验收
- [DATABASE_DESIGN.md](./docs/DATABASE_DESIGN.md) — 16 表库表设计
- [docs/api/API.md](./docs/api/API.md) — 接口清单与鉴权矩阵
- [docs/adr/](./docs/adr/) — 架构决策记录（ADR）
- [AGENTS.md](./AGENTS.md) — 仓库协作契约（含目录规范、红线、陷阱）

## 路线状态

当前为 **v1.0.0 首次发布**。核心功能（上传、列表、详情、筛选、备份、体检）完全可用，见 [REQUIREMENTS.md](./docs/REQUIREMENTS.md)。

## 许可

[GPL-3.0](./LICENSE) © Paperly contributors
