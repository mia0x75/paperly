# 前端路由整理文档

> **版本**：v1.0.0
> **最后更新**：2026-10-29
> **权威性**：本文档是前端路由 / 布局 / 守卫的权威源，以 `frontend/src/router/index.ts` 为准。

> 来源：`frontend/src/router/index.ts`
> 仅记录前端路由定义，不涵盖后端 API。守卫说明见同文件注释。

## 概览
- Router：`createWebHistory()`（HTML5 history）
- 测试用导出：`export const routes: RouteRecordRaw[]`（运行时行为不变）

## 1. 家长端（`/`，`meta: { audience: "parent" }`，`ParentLayout.vue`）

| 路径 | name | 组件 | 说明 |
|---|---|---|---|
| `/` | `home` | `pages/HomePage.vue` | 首页 |
| `/exams` | `exam-list` | `pages/ExamListPage.vue` | 试卷列表 |
| `/exams/upload` | `exam-create` | `pages/ExamCreatePage.vue` | 上传试卷（新建） |
| `/exams/:id` | `exam-viewer` | `pages/ExamViewerPage.vue` | 试卷详情（查看/编辑） |
| `/messages` | `messages` | `pages/MessageListPage.vue` | 通知列表（分页/删除/已读圆点） |
| `/kids` | `kids` | `pages/KidsPage.vue` | 孩子管理 |
| `/kids/:id` | `kid-detail` | `pages/KidDetailPage.vue` | 孩子详情 |
| `/storage` | `storage` | `pages/settings/StoragePage.vue` | 存储设置 |
| `/image` | `image-settings` | `pages/settings/ImageSettingsPage.vue` | 图像设置 |
| `/kids-auth` | `kids-auth` | `pages/settings/KidsAuthPage.vue` | 孩子与免密授权设置 |
| `/parents` | `parents` | `pages/settings/ParentsPage.vue` | 家长账号/设置 |
| `/tasks` | `tasks-settings` | `pages/settings/TasksPage.vue` | 任务设置 |
| `/subjects` | `subjects` | `pages/settings/SubjectsPage.vue` | 科目设置 |
| `/knowledge-points` | `knowledge-points` | `pages/settings/KnowledgePointsPage.vue` | 知识点设置 |
| `/system` | `system` | `pages/settings/SystemPage.vue` | 系统设置 |

## 2. 孩子端（`/kid`，`KidLayout.vue`）

使用**同一份**页面组件；子路由按需标注 `meta: { audience: "kid" }`。

| 路径 | name | 组件 | `meta` | 说明 |
|---|---|---|---|---|
| `/kid` | `kid-home` | `pages/KidHomePage.vue` | `audience: "kid"` | 孩子首页（Dashboard） |
| `/kid/exams` | `kid-exams` | `pages/ExamListPage.vue` | `audience: "kid"` | 孩子端试卷列表 |
| `/kid/exams/upload` | `kid-exam-create` | `pages/ExamCreatePage.vue` | `audience: "kid"` | 孩子端上传试卷 |
| `/kid/exams/:id` | `kid-exam-viewer` | `pages/ExamViewerPage.vue` | `audience: "kid"` | 孩子端试卷详情 |
| `/kid/messages` | `kid-messages` | `pages/MessageListPage.vue` | `audience: "kid"` | 孩子端通知列表（与家长端同一页面组件） |
| `/kid/subjects` | `kid-subjects` | `pages/settings/SubjectsPage.vue` | `audience: "kid"` | 孩子端科目（共用设置页） |
| `/kid/knowledge-points` | `kid-knowledge-points` | `pages/settings/KnowledgePointsPage.vue` | `audience: "kid"` | 孩子端知识点（共用设置页） |

> 注：`/kid/*` 子路由全部显式标记 `audience: "kid"`。

## 3. 旧链接重定向（`/kid/:accessCode*`）
| 路径 | name | 重定向到 | 说明 |
|---|---|---|---|
| `/kid/:accessCode` | `kid-entry` | `/invite/${encodeURIComponent(accessCode)}` | P5b 起旧分享链接重定向到邀请页 |
| `/kid/:accessCode/:rest(.*)*` | `kid-entry-deep` | `/invite/${encodeURIComponent(accessCode)}` | 带深链的旧链接一并重定向到邀请页 |

## 4. 独立无导航页（脱离 Layout）
| 路径 | name | 组件 | 说明 |
|---|---|---|---|
| `/welcome` | `welcome` | `pages/WelcomePage.vue` | 首启引导页（by `onboardingGuard`） |
| `/login` | `login` | `pages/LoginPage.vue` | 家长登录页（by `parentGuard`） |
| `/invite/:code` | `invite` | `pages/InvitePage.vue` | 邀请设置页（校验邀请码、设 PIN、写孩子 token） |
| `/kid/login` | `kid-login` | `pages/KidLoginPage.vue` | 孩子登录页（顶层独立，非 `/kid` 子路由） |

## 5. SPA Fallback
- `/:pathMatch(.*)*` → 重定向到 `/`

## 6. 导航守卫（顺序：markRouteStart → onboardingGuard → kidGuard → parentGuard）
- `onboardingGuard`：首启未完成强制跳 `/welcome`；`/welcome` 放行；接口失败时**放行**
- `kidGuard`：基于 `readParentToken()` / `readKidToken()` 判断设备类型；`/kid` 树优先规则见代码注释（家长 token 优先防环）
- `parentGuard`：家长端需 `audience=parent` 且有家长 token；精确判断 `/kid/*`（`isKidPath`）避免误拦

## 7. 备注
- 家长端与孩子端**复用**大部分页面组件（未复制页面）
- 孩子入口 `/kid/login` 注册为**顶层**路由，防止挂在 KidLayout 下先绘制顶栏
- 测试可直接引用 `routes` 导出做断言
