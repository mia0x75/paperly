# API 路由归属总表

> **版本**：v1.0.0
> **最后更新**：2026-10-10
> **权威性**：本文档是 API 路径 / 鉴权归属的权威源，以各 handler 的 `RegisterRoutes` + `cmd/server/main.go` 挂载为准。
> **去重原则**：请求响应字段见各 DTO；业务规则见 `docs/REQUIREMENTS.md`；表结构见 `docs/DATABASE_DESIGN.md`。

## 一、白名单（匿名，无需 JWT）

| # | 方法 | 路径                        | 说明            |
|---|------|-----------------------------|-----------------|
| 1 | POST | `/api/auth/login`           | 家长登录        |
| 2 | POST | `/api/kid/auth/login`       | 孩子登录        |
| 3 | POST | `/api/kid/invite/verify`    | 邀请码校验      |
| 4 | POST | `/api/kid/invite/setup`     | 邀请码设 PIN    |
| 5 | GET  | `/api/auth/parent-accounts` | 家长账号列表    |
| 6 | GET  | `/api/auth/kid-accounts`    | 孩子账号列表    |
| 7 | GET  | `/api/onboarding/status` | Onboarding 状态 |
| 8 | POST | `/api/onboarding/complete` | Onboarding 完成 |

---

## 二、家长专属（`requireParent`）

| #  | 方法   | 路径                                     | 说明 |
|----|--------|------------------------------------------|------|
| 1  | GET    | `/api/users`                             | 家长列表 |
| 2  | POST   | `/api/users`                             | 新增家长 |
| 3  | POST   | `/api/me/rename`                         | 改本人昵称 |
| 4  | GET    | `/api/kids`                              | 孩子列表 |
| 5  | POST   | `/api/kids`                              | 新增孩子 |
| 6  | GET    | `/api/kids/:id`                          | 孩子详情 |
| 7  | PUT    | `/api/kids/:id`                          | 改孩子 |
| 8  | DELETE | `/api/kids/:id`                          | 删孩子 |
| 9  | POST   | `/api/kids/:id/access_code/reset`        | 重置邀请码 |
| 10 | POST   | `/api/kids/:id/pin/reset`                | 重置孩子 PIN |
| 11 | POST   | `/api/kids/:id/restore`                  | 恢复孩子 |
| 12 | GET    | `/api/settings/image`                    | 图片压缩设置 |
| 13 | PUT    | `/api/settings/image`                    | 图片压缩设置 |
| 14 | GET    | `/api/tasks`                             | 任务列表 |
| 15 | GET    | `/api/tasks/:id`                         | 任务详情 |
| 16 | POST   | `/api/tasks/:id/retry`                   | 重试任务 |
| 17 | POST   | `/api/tasks/clear-finished`              | 清已完成任务 |
| 18 | GET    | `/api/dashboard`                         | 家长 Dashboard |
| 19 | GET    | `/api/dashboard/persistence`             | 坚持度（`trend_days` 缺省 7、仅收 7/30；空 items 序列化为 `[]`） |
| 20 | GET    | `/api/dashboard/distribution`            | 试卷分布（`kid_id` 可选，省略为全部聚合） |
| 21 | GET    | `/api/dashboard/counts`                  | 首行 KPI 计数（`kid_id` 可选，省略为全部聚合） |
| 22 | GET    | `/api/metrics/summary`                   | 体检数据 |
| 23 | GET    | `/api/admin/backups`                     | 备份列表 |
| 24 | GET    | `/api/admin/backups/:file_name/download` | 下载备份 |
| 25 | GET    | `/api/admin/cursors`                     | 游标列表 |
| 26 | GET    | `/api/admin/health`                      | 系统健康 |
| 27 | GET    | `/api/admin/migrations`                  | 迁移历史 |
| 28 | GET    | `/api/admin/seed`                        | seed 列表 |
| 29 | POST   | `/api/admin/backup`                      | 立即备份 |
| 30 | POST   | `/api/admin/seed/ensure`                 | 确保 seed |

---

## 三、孩子专属（`requireKid`）

| # | 方法 | 路径 | 说明 |
|---|------|------|------|
| 1 | GET  | `/api/kid/dashboard` | 孩子 Dashboard |

---

## 四、两端共用（`requireParent` OR `requireKid`）

| # | 方法    | 路径                         | 说明 |
|---|---------|------------------------------|------|
|  1 | GET    | `/api/me`                    | 当前身份（按 token 受众返回家长/孩子） |
|  2 | POST   | `/api/me/pin`                | 改本人 PIN（家长/孩子按 token 分流） |
|  3 | GET    | `/api/exams`                 | 试卷列表（孩子端强制自己的） |
|  4 | GET    | `/api/exams/:id`             | 试卷详情（孩子端校验归属） |
|  5 | POST   | `/api/exams/upload`          | 上传（孩子端强制 kid_id） |
|  6 | PUT    | `/api/exams/:id`             | 编辑（孩子端校验归属） |
|  7 | DELETE | `/api/exams/:id`             | 软删（孩子端校验归属） |
| 8  | GET    | `/api/pages/:page_id/file`   | 页图（孩子端校验归属） |
|  9 | GET    | `/api/messages`              | 通知列表（按 typ 过滤） |
| 10 | GET    | `/api/messages/unread-count` | 未读数（按 typ 过滤） |
| 11 | POST   | `/api/messages/:id/read`     | 标已读（按 typ 约束） |
| 12 | POST   | `/api/messages/read-all`     | 全标已读（按 typ 约束） |
| 13 | GET    | `/api/subjects`              | 科目列表 |
| 14 | POST   | `/api/subjects`              | 新增科目 |
| 15 | PUT    | `/api/subjects/:id`          | 改科目 |
| 16 | DELETE | `/api/subjects/:id`          | 删科目 |
| 17 | GET    | `/api/knowledge_points`      | 知识点列表 |
| 18 | POST   | `/api/knowledge_points`      | 新增知识点 |
| 19 | PUT    | `/api/knowledge_points/:id`  | 改知识点 |
| 20 | DELETE | `/api/knowledge_points/:id`  | 删知识点 |
| 21 | GET    | `/api/metrics/storage-trend` | 存储增长趋势（`days` 缺省 30、钳制 [1,365]；缺采日不补行） |

---

## 五、汇总

| 类别     | 条数   |
|----------|--------|
| 白名单 | 8 |
| 家长专属 | 30 |
| 孩子专属 | 1 |
| 两端共用 | 21 |
| **合计** | **60** |

---

## 六、越权响应与过滤规则（速查）

| 场景                            | 行为                                    |
|---------------------------------|-----------------------------------------|
| 无 token 访问非白名单           | 401                                     |
| 家长 token 访问孩子专属         | 401                                     |
| 孩子 token 访问家长专属         | 401                                     |
| 孩子 token 访问 `/api/exams`    | 强制 `kid_id=当前孩子`                  |
| 孩子 token 访问他人试卷详情     | 404                                     |
| 家长 token 访问 `/api/messages` | 只看 `audience=parent`                  |
| 孩子 token 访问 `/api/messages` | 只看 `audience=kid AND kid_id=当前孩子` |
| 401 响应                        | 不区分"未登录"与"角色不符"              |

