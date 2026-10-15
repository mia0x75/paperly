# ADR 0004: users 由单账号放宽为多家长 + 首启引导（onboarding）

## 状态

已接受（人类裁定，2026-10-06）。同步修订 `DATABASE_DESIGN.md` 3.1 节与 `AGENTS.md`（首启不再自动建号）。

## 背景

原设计「BOX 单机只有一个家长账号」：`users` 表只有一行，启动时经
`EnsureDefault` 惰性建出「家长管理员」，无列表/新建端点。需求的
首启引导（onboarding）要求：

1. `/welcome` 独立无导航页，三步：欢迎 → 添加家长（≥1）→ 添加孩子（≥1）→ 跳 `/`。
2. 前端路由守卫在未完成引导时强制跳 `/welcome`。

盘点发现与现状冲突：

- **无「添加家长」弹层**：设置页只改单个账号的昵称/PIN，不存在创建流程；
- **`users` 无列表/计数 API**：仅 `GET /api/me`（当前账号）；
- **启动即 `EnsureDefault` 建号**：`users` 永不为空，onboarding「家长列表可空」的前提不成立。

按任务流程停止并上报，人类裁定**方案 B：新建多家长流程，后续会继续使用**。

## 决策

1. **多家长**：`users` 表支持多行（schema 本就是 `AUTOINCREMENT` + `display_name UNIQUE`，
   无需迁移）。
   - `domain.UserRepository` 新增 `List` / `Insert` / `ExistsByDisplayName`；
   - `GET /api/users` 列全部家长，`POST /api/users` 登记新家长（昵称非空、≤24 rune、
     全表唯一；PIN 4~6 位数字选填，慢哈希）；
   - `/api/me` 固定读写「id 最小者」作默认家长（无登录态表，此语义最接近"当前账号"）。
2. **去掉 `EnsureDefault`**：首启 `users` 为空，由 onboarding 引导登记首位家长；
   `/api/me` 在引导完成前 404 属预期降级，不再静默建号掩盖问题。
3. **一次性完成标记走既有 `cursors` 表**（与 seed 导入机制同一张表，任务名
   `onboarding_done`，`last_status=2`=Completed）：
   - `GET /api/onboarding/status` → `{done: bool}`；
   - `POST /api/onboarding/complete` → 校验 `users≥1` 且 `kids≥1` 后写游标。
4. **完成判定是复合的**：游标存在且 Completed **且** `users≥1` **且** `kids≥1`。
   用户清空数据后，标记仍在但实体为空，守卫会重新把用户拉回 `/welcome`，防止空壳系统裸奔。
5. **前端**：`/welcome` 独立路由（无导航、无 Layout）；`beforeEach` 守卫查 `done`，
   非 `/welcome` 一律拦截；复用「添加孩子」弹层（`KidCreateDialog`），新建
   `UserCreateDialog` 登记家长。守卫请求失败时放行（不因后端抖动把用户锁死在引导页）。

## 理由

1. 「Box 单机单账号」是纯简化假设，不是硬约束，`UNIQUE(display_name)` 与自增主键
   从一开始就支持多行。为 onboarding 的需求（添加家长列表）放宽最便宜。
2. 去掉 `EnsureDefault` 让「首启引导」名副其实：空库首启必须先建家长，而不是
   弹出一个已经建好的「家长管理员」——否则引导第 2 步成了无意义的形式。
3. 完成标记复用 `cursors` 而非新建表：`VACUUM INTO` 备份、体检页展示、迁移记录
   （`schema_version`）全部不用动。

## 后果

- `users` 不再恒有默认账号；依赖 `/api/me` 的页面在引导完成前
  显示「账号不存在」，引导完成后恢复。
- 新增接口 `GET/POST /api/users`，`GET/POST /api/onboarding/*`；API 文档需同步。
- 品牌隔离与既有枚举/三视角 DTO 规范不受影响。
