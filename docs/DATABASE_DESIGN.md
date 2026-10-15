# 数据库设计与规范文档

> **版本**：v1.0.0
> **最后更新**：2026-10-10
> **权威性**：本文档是 16 张表的唯一权威源（表名 / 字段 / 索引 / 枚举）。
> **变更影响**：任何 schema 改动必须同步本文档 + `migrations/`。

> **引擎规范**：纯 Go 驱动 `modernc.org/sqlite`（无 CGO），WAL 模式，单机文件型数据库。
>
> **架构边界**：无多租户（单实例单家庭），全表无 `tenant_id`。
>
> **本设计以「单实例单家庭」为不可动摇的前提。** 任何字段、索引、约束，只服务于单家庭场景；不预留多租户 / 跨家庭 / SaaS 迁移路径。遇「要不要为多租户预留」的问题，答案一律为**不预留**（见 `REQUIREMENTS.md` 单实例单家庭原则）。
>
> **枚举规范**（两类）：
> - **TEXT 枚举**：原则上禁止使用。
> - **INT 枚举**：存储值用整数代码（如 `exam_type=2`）；Go 侧定义常量（如 `ExamTypeMonthly=2`）；DTO 层映射中文。
> - 两类枚举均需 `ParseXxx` 防御：未知值降级为 `Other`（或默认值），绝不 panic。
>

## 一、存储选型与核心数据约束

1. **并发控制与事务模型**：

   * 数据库连接池配置：单写连接 `SetMaxOpenConns(1)` + `busy_timeout(5000)` + `_txlock=immediate`。

   * 并发策略：写池串行（单连接 + `_txlock=immediate` + `busy_timeout`），写优先级为 `全量重算链 > 题级链 > 考试级链 > 每日维护`。

   * 冲突避免：单行记录使用乐观版本号机制（`UPDATE ... WHERE id=? AND row_version=?`）。


2. **状态规范**：

   * **三态**：仅 `kids`（1 - `active` / 2 - `graduated` / 3 - `deleted`）——毕业保留历史，软删可恢复。

   * **两态（INT）**：`exams` / `questions`（1 - `active` / 0 - `deleted`）——无毕业语义，软删可恢复。

   * **两态（TEXT）**：`users`（1 - `active` / 0 - `disabled`）——家长账号无软删，`disabled` 表"停用"。

3. **备份与体检**：

   * 采用 SQLite 原生 `VACUUM INTO` 语法实施在线不停机原子快照备份。

4. **软删过滤**：

   * 凡经 `exams` 推导的查询（聚合、错题本、掌握度、诊断），**统一加 `AND e.status=1`**——`exams` 软删不动子表，靠查询层过滤。


# `users` 表设计（家长账号）

## 一、业务用途

管理家庭内的家长账号，实施 PIN 登录认证与"改 PIN 踢下线"能力。

**核心场景**：
- 家长登录后访问管理端（试卷 / 孩子 / 系统）
- 未登录访问 → 跳登录页
- 家庭内 1~3 个家长账号（爸爸、妈妈……），权限一致
- 登录方式：**选账号（display_name）+ 输 PIN**，无需记忆用户名
- 改 PIN 时，旧 JWT 立即失效（`pin_version` 机制）

**边界**：
- 无角色分级（家庭内所有家长权限一致）
- 无软删（家长账号仅 `active` / `disabled`，真要删就物理删）
- 无"主密码"（PIN 是唯一凭据）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 家长账号唯一主键 |
| `display_name` | `TEXT` | NOT NULL, UNIQUE | 无 | 显示名（如"爸爸"、"妈妈"），**兼作登录标识**，家庭内唯一 |
| `pin_hash` | `TEXT` | NOT NULL | 无 | 登录 PIN 的慢哈希值（bcrypt cost ≥ 12 / argon2id / scrypt），**唯一凭据** |
| `pin_version` | `INTEGER` | NOT NULL | `1` | PIN 版本号。**修改 PIN 时递增**；JWT 中携带此值，验签后与 DB 比对，不等则拒签（实现"改 PIN 踢下线"） |
| `status` | `INTEGER` | NOT NULL | `1` | 账号状态：1（正常）/ 0（停用）。**无 `deleted`**——真要删则物理删 |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 账号创建时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 账号最后更新时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE users (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    display_name TEXT NOT NULL UNIQUE,
    pin_hash     TEXT NOT NULL,
    pin_version  INTEGER NOT NULL DEFAULT 1,
    status       INTEGER NOT NULL DEFAULT 1,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## 四、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 作为 JWT 载荷的 `sub`；关联 `exams.uploaded_by`（=1 家长）的审计信息（家庭内不区分具体家长） |
| `display_name` | 登录页**账号选择列表**（radio / select）的数据源；UI 上显示"当前登录：爸爸" |
| `pin_hash` | 登录时 `bcrypt.CompareHashAndPassword` 校验；PIN 规则：4~6 位数字 |
| `pin_version` | JWT 载荷中带 `pv`；每次请求验签后查 DB `pin_version`，不等则 401；**改 PIN 时 `pin_version++`** |
| `status` | 登录时校验：`disabled` 拒登；已登录用户的 token 每次请求也校验状态 |
| `created_at` | 账号列表展示"创建于 X"；审计 |
| `updated_at` | 账号信息修改时间；审计 |

---

## 五、应用流程

### 5.1 首启向导

```
1. 检测 users 表为空
2. 引导创建第一个家长账号：
   - display_name: "爸爸"（默认，可修改）
   - PIN: 4~6 位数字（二次确认）
3. INSERT users(display_name, pin_hash, pin_version=1, status='active')
4. 自动登录（签发 JWT）
```

### 5.2 登录

```
1. GET /api/auth/parent-accounts → 返回 [{id, display_name}, ...]
2. 用户选择账号 + 输入 PIN
3. POST /api/auth/login {user_id, pin}
4. 服务端校验：
   - users.status == 1
   - bcrypt.CompareHashAndPassword(pin_hash, pin) == nil
   - 登录限流（连续 5 次失败锁定 15 分钟，见 5.4 节）
5. 签发 JWT：
   {
     "sub": user_id,
     "dn": display_name,
     "pv": pin_version,
     "exp": now + 30d
   }
6. 客户端存 JWT（localStorage / cookie）
```

### 5.3 鉴权

```
每次请求：
1. 从 Authorization: Bearer <jwt> 提取 token
2. 验签（HMAC / RSA）
3. 提取 sub / pv
4. 查 DB：SELECT pin_version, status FROM users WHERE id = sub
5. 校验：
   - status == 1
   - db.pin_version == jwt.pv
6. 通过 → 继续；不通过 → 401
```

### 5.4 登录限流

```
策略：连续 5 次 PIN 错误 → 锁定 15 分钟
存储：内存（map[user_id]{fail_count, locked_until}）—— 不建表
理由：单机本地，重启丢失可接受
```

### 5.5 改 PIN（踢下线）

```
1. POST /api/auth/change-pin {old_pin, new_pin}
2. 校验 old_pin
3. UPDATE users SET
     pin_hash = bcrypt(new_pin),
     pin_version = pin_version + 1,
     updated_at = CURRENT_TIMESTAMP
   WHERE id = ?
4. 所有旧 JWT 的 pv 与 DB 不符 → 下次请求 401
5. 当前会话重新签发 JWT（新 pv）
```

### 5.6 多家长

```
1. POST /api/users {display_name, pin}
2. 校验 display_name 唯一
3. INSERT
4. 所有家长权限一致（无 role 分级）
```

---

## 六、设计决策记录

| 决策 | 理由 |
|------|------|
| **无 `username`** | 家庭场景 1~3 个账号；选 `display_name` + 输 PIN 更自然，无需记忆用户名 |
| **`display_name` NOT NULL UNIQUE** | 兼作登录标识；家庭内不允许重名 |
| **无 `role`** | 家庭内家长权限一致；分级是过度设计 |
| **`status` 用 `1`/`0`** | 家长账号不存在"软删"语义；`0` 表达"停用"更精确 |
| **`pin_hash` 唯一凭据，无 `password_hash`** | PIN 是唯一登录方式；独立"密码"字段是多余的 |
| **`pin_version`** | 支持"改 PIN 踢下线"；JWT 无状态的前提下实现主动失效 |
| **登录限流不建表** | 单机本地，内存 map 足够；重启丢失可接受 |
| **JWT 有效期 30 天** | 家庭场景，减少频繁登录 |

---

## 七、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `exams` | 无直接外键 | `exams.uploaded_by=1`（家长）时，**不记录具体哪个家长**（家庭内无需区分） |
| `tasks` | 无直接外键 | 定时任务的触发与家长无关 |
| — | — | `users` 是独立根实体，不被其他表外键引用 |

---

## 八、安全要点

| 项 | 措施 |
|----|------|
| PIN 存储 | bcrypt cost ≥ 12（单次验证 ~100ms） |
| PIN 规则 | 4~6 位数字 |
| 登录限流 | 5 次失败锁 15 分钟（内存） |
| JWT 有效期 | 30 天 |
| JWT 载荷 | `sub` / `dn` / `pv` / `exp` |
| 主动失效 | `pin_version` 机制 |
| 首启强制 PIN | `pin_hash NOT NULL`，无"空密码"状态 |

---

## 九、索引

- `display_name` 的 UNIQUE 约束已隐含索引（用于登录页账号列表查询）

# `kids` 表设计（孩子档案）

## 一、业务用途

管理家庭内孩子的个人档案、学生端免登录访问码、可选 PIN 凭证。

**核心场景**：
- 家长在管理端创建 / 编辑 / 删除孩子档案
- 孩子通过 `access_code` 免登录进入 `/kid/{code}`（可选 PIN 二次校验）
- 试卷归属某个孩子（`exams.kid_id`）
- 后续 OCR 阶段：`questions` 经 `exams.kid_id` 间接关联该孩子（`questions` 本身不存 `kid_id`）

**边界**：
- 无"学生"教育术语（家庭语境用"孩子"）
- 无班级管理（家庭内不需要）
- 无"学校"字段（不涉及家校关系）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 孩子档案唯一主键 |
| `name` | `TEXT` | NOT NULL, UNIQUE | 无 | 孩子姓名 / 昵称（**全表唯一**，含已删除/已毕业） |
| `grade` | `INTEGER` | NOT NULL | 无 | 当前年级（1~9 义务教育；10~12 高中）。**每年 9 月 1 日凌晨由 `tasks` 自动 +1**，家长可手动覆盖 |
| `track` | `INTEGER` | NOT NULL | `0` | 文理分科：`0`（未分科）/ `1`（文科）/ `2`（理科）。**仅高中（年级 ≥ 10）参与科目过滤**，初中及以下存了也不生效；驱动 `subjects.mask` 高 2 位过滤（上传页 / 雷达图）。年级自动升到 10 时向家长推送 `MessageKindGrade10Reminder` 提醒设置 |
| `gender` | `INTEGER` | NULL | NULL | 性别：`1`（男）/ `0`（女）（**不设 other**）；NULL = 未填写 |
| `avatar` | `TEXT` | NULL | NULL | 头像文件相对路径（可选） |
| `access_code` | `TEXT` | NOT NULL, UNIQUE | 无 | 学生端免登录固定 8 位随机码：从 31 字符集 `[2-9a-hj-km-np-z]`（数字 2-9 + 字母 a-h,j-k,m-n,p-z；排除 `0/o/1/l/i`）随机取 **8 位**；熵 = log2(31^8) ≈ **39.6 bit**（约 40 bit） |
| `pin_hash` | `TEXT` | NOT NULL | 无 | PIN 慢哈希（bcrypt）；建孩子时未设 PIN 则写占位值 `'!'`（非 bcrypt，任何 PIN 登录恒失败） |
| `pin_version` | `INTEGER` | NOT NULL | `1` | PIN 版本号（同 `users`——改 PIN 踢下线） |
| `status` | `INTEGER` | NOT NULL | `1` | 状态：`1`（在读）/ `2`（毕业，只读）/ `3`（软删） |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 档案创建时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 档案最后更新时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE kids (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    grade        INTEGER NOT NULL,
    gender       INTEGER,
    avatar       TEXT,
    access_code  TEXT NOT NULL UNIQUE,
    pin_hash     TEXT NOT NULL,
    pin_version  INTEGER NOT NULL DEFAULT 1,
    status       INTEGER NOT NULL DEFAULT 1,
    track        INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (grade BETWEEN 1 AND 12),
    CHECK (gender IS NULL OR gender IN (0, 1)),
    CHECK (status IN (1, 2, 3)),
    CHECK (track IN (0, 1, 2))
);

CREATE INDEX idx_kids_grade ON kids(grade);
-- 姓名全表唯一（含已删除/已毕业）
CREATE UNIQUE INDEX idx_kids_name ON kids(name);
```

---

## 四、`grade` 取值规范

| 值 | 含义 | 学段 |
|----|------|------|
| 1 | 小学一年级 | 义务教育 |
| 2 | 小学二年级 | 义务教育 |
| 3 | 小学三年级 | 义务教育 |
| 4 | 小学四年级 | 义务教育 |
| 5 | 小学五年级 | 义务教育 |
| 6 | 小学六年级 | 义务教育 |
| 7 | 初中一年级（初一） | 义务教育 |
| 8 | 初中二年级（初二） | 义务教育 |
| 9 | 初中三年级（初三） | 义务教育 |
| 10 | 高中一年级（高一） | 高中 |
| 11 | 高中二年级（高二） | 高中 |
| 12 | 高中三年级（高三） | 高中 |

**UI 显示映射**：
```
1~6   → "小学一年级" ~ "小学六年级"
7~9   → "初一" / "初二" / "初三"
10~12 → "高一" / "高二" / "高三"
```

**毕业规则**：`grade = 12` 后学年结束 → 自动 `status='graduated'`。

---

## 五、`grade` 自动推进机制

**触发时机**：每年 **9 月 1 日凌晨**（开学日）。

**流程**：

```
1. 定时任务检测当前日期 == 9 月 1 日
2. 对每个 status=1 的孩子入队（grade=12 也入队，用于毕业判定，见 step 3.d）：
   INSERT INTO tasks (kind, priority, status, detail)
   VALUES (2, 80, 0, json_object(
       'kid_id', ?,
       'enrolled_year', ?,    -- 入队学年（如 2026）
       'enrolled_grade', ?,   -- 入队时该孩子的 grade
       'target_grade', ?      -- 入队即算好本学年目标 = enrolled_grade + 1
   ));
   -- kind=2: advance_grade
   -- 不用 suggested_grade 冻死值；改用 enrolled_year + target_grade（入队算好本学年目标），消费时按 enrolled_year 差值跨年补足
3. Worker 消费（Handler，逐条处理，幂等）——**改 kids + 置 task.done + 发 messages 三者在同一事务提交**；崩溃则全回滚，任务保持 pending 下次重试。
   对每一条 status=0 AND kind=2 的 task：
   a. 解析 detail：kid_id, enrolled_year, enrolled_grade, target_grade
   b. 实时算**原始目标** raw_target = target_grade + (当前学年 - enrolled_year)
      （当前学年 = 现日期 ≥ 9/1 ? 当年 : 上一年）
   c. 幂等门（唯一口径）：**仅当 `kids.grade == enrolled_grade`**（家长入队后未手改）才推进；否则跳过（尊重家长手改，无论调高或调低）
   d. 若需推进，按 raw_target 分流：
      · raw_target ≤ 12 →
        UPDATE kids SET grade = raw_target, updated_at = CURRENT_TIMESTAMP
        WHERE id=? AND status=1
      · raw_target > 12 →
        UPDATE kids SET status = 2, updated_at = CURRENT_TIMESTAMP
        WHERE id=? AND status=1
        （grade 保持 12 不变；毕业由"该学年本应 +1 时 raw_target 越过 12"触发）
   e. UPDATE tasks SET status=2(done), finished_at=now WHERE id=task_id
      -- 关键：消费后必置 done，否则任务永久 pending、每次空转、永不清理
    f. 若实际推进了 → 写家长通知（**文案与 `messages` 表 7.1 节一致**：`kind=1`，`title`/`body`/`link` 见该节）
4. **边界说明**：
   · grade ≤ 11：本学年推进到 grade+1
   · grade = 12：本学年结束 → 毕业（status=2）
   · 家长手改 grade 后，幂等门（step 3.c）跳过，尊重家长决策
5. **家长可随时手动覆盖 grade**（见第二章字段说明 / 第八章决策）——手改后入队任务自动跳过（口径见 step 3.c）
```

**写入 `tasks` 表**（而非直接更新）的理由：
- 与 `tasks` 基础设施一致
- 若任务失败可重试
- 推进成功后派生 `messages` 通知家长（`MessageKindGradeAdvanced`）；升入初一 / 初三 / 高一分别追加 `MessageKindGrade7Reminder`（确认科目完整）/ `MessageKindGrade9Reminder`（结课隐藏）/ `MessageKindGrade10Reminder`（设置文理科）

**幂等保证**：`tasks` 表**无**唯一约束（Payload-Only 载荷，幂等由 Handler 保证）——消费时（见 step 3.c）**仅当 `kids.grade == enrolled_grade`** 才推进；重复消费/家长手改后均跳过（唯一口径见 step 3.c，本条不另设条件）。

**时区**：使用服务器本地时间（Box 单机本地）。

---

## 六、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 被 `exams.kid_id` 引用；未来 `levels.kid_id` / `gaps.kid_id` 引用（`questions` 经 `exams` JOIN 推导，不直连） |
| `name` | UI 显示；学生端首页问候"你好，小明"；家长端列表 |
| `grade` | 家长端筛选；UI 显示"初三"；每年 9/1 自动 +1 |
| `gender` | 可选（INTEGER 1=男 / 0=女 / NULL=未填）；UI 头像默认选择；无业务逻辑 |
| `avatar` | 学生端头像展示；家长端列表缩略图 |
| `access_code` | 学生端入口 `/kid/{code}`；家长可重置（旧码失效） |
| `pin_hash` | 建孩子时写占位值；首次通过 `/invite/:code` 设真 PIN；已设 PIN 后凭 PIN 校验进入 |
| `pin_version` | 改 PIN 时递增；JWT 中带 `pv`，验签后比对；实现"改 PIN 踢下线" |
| `status` | `1` 全部可见；`2` 只读可见（历史数据保留）；`3` 可见（标记软删） |
| `created_at` | 列表展示；审计 |
| `updated_at` | 修改时间；审计 |

---

## 七、应用流程

### 7.1 创建孩子

```
1. 家长在管理端点"添加孩子"
2. 填：姓名、年级（1~12）、性别（可选）、头像（可选）
3. 服务端生成唯一 access_code：
   - 字符集 [2-9a-hj-km-np-z]（31 字符：2-9, a-h, j-k, m-n, p-z；排除 0/o/1/l/i）
   - 长度 8 位
   - 熵 = log2(31^8) ≈ 39.6 bit
   - 查重（UNIQUE access_code）冲突则重试（≤ 10 次）
4. INSERT kids(name, grade, gender, avatar, access_code, pin_hash='!'(占位，未设 PIN), pin_version=1, status=1)
5. 返回 access_code（一次性展示给家长）
```

### 7.2 学生端访问（免登录）

```
1. 孩子打开 /kid/{access_code}
2. 服务端查 kids WHERE access_code = ? AND status IN (1, 2)
3. 若 pin_hash 为占位值 `'!'`（未设 PIN）：孩子走 `/invite/:code` 设真 PIN，成功后直接签发 JWT 进入
   若为真 bcrypt 哈希：
   - 提示输入 PIN
   - 校验 bcrypt.CompareHashAndPassword
4. 签发孩子端 JWT：
   {
     "sub": kid_id,
     "typ": "kid",
     "pv": pin_version,
     "exp": now + 30d
   }
5. 进入孩子端首页（只读 + 上传自己试卷）
```

### 7.3 家长重置 access_code

```
1. POST /api/kids/:id/access-code/reset
2. 服务端重新生成唯一 access_code
3. UPDATE kids SET access_code = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
4. 旧 access_code 立即失效（URL 打不开）
5. 返回新码给家长
```

### 7.4 家长重置孩子 PIN

```
1. POST /api/kids/:id/pin/reset {new_pin?}
2. 若 new_pin 提供：
   - 校验 4~6 位数字
   - UPDATE kids SET pin_hash = bcrypt(new_pin), pin_version = pin_version + 1
3. 若 new_pin 为空：
   - UPDATE kids SET pin_hash = '!'（占位值）, pin_version = pin_version + 1
   （重置为占位值，孩子须重走邀请链接设真 PIN 后进入）
4. 旧 JWT 因 pv 不符失效
```

### 7.5 软删 / 毕业

**原则上不允许删除孩子档案**。家庭版路径：
- **毕业**：`UPDATE kids SET status=2`（grade=12 时自动；也可手动）——只读保留
- **软删**：`UPDATE kids SET status=3`——不可见但保留数据
- **恢复**：`UPDATE kids SET status=1`

**不提供"物理删除孩子"的 UI**——若家长确要清空实例，直接清理 DB 文件与 `assets/` 目录（Docker 卷）。

**物理删 `kids` 行**（如直改库）时 FK 行为（保留供参考）：
- `exams`：`ON DELETE SET NULL`——试卷留库、转"未归属"
- `levels`/`gaps`/`messages`：`ON DELETE CASCADE`——随孩子清空
- `tasks`：不主动清，Handler 消费时查不到 `kids` → 标 `skipped`

### 7.6 年级推进（每年 9 月 1 日凌晨）

见第五章。

---

## 八、设计决策记录

| 决策 | 理由 |
|------|------|
| **表名 `kids`（非 `students`）** | 家庭语境用"孩子"更自然；避免与"学校学生"混淆 |
| **无 `class_name` / `student_no`** | 家庭工具不需要学校管理字段 |
| **无 `birthday`** | 家长知道孩子年龄；不需要额外字段 |
| **`gender` 仅 1（男）/ 0（女）** | 不设 other；无意义填"男/女"外选项（用户已明确拒绝）；NULL = 未填写 |
| **`grade` 用整数 1~12** | 覆盖小学~高中；排序自然；UI 映射中文显示 |
| **`grade` 每年 9/1 自动 +1** | 通过 `tasks` 表入队；家长可手动覆盖 |
| **`grade=12` 后自动 graduated** | 高三结束即毕业；无需手动操作 |
| **`access_code` 8 位 + 31 字符集** | 熵 ≈ 40 bit（31^8 ≈ 39.6）；排除 `0/o/1/l/i` 防手抄错 |
| **`pin_hash` NOT NULL（占位 `'!'`）** | 未设 PIN 时存占位值（非 bcrypt，任何 PIN 登录恒失败）；首次进入走邀请链接设真 PIN |
| **`pin_version`** | 与 `users` 一致——改 PIN 踢下线 |
| **`status` 三态** | `1` / `2` / `3`；毕业保留历史，软删可恢复 |
| **无 `user_id` 归属** | 单家庭所有家长共享所有孩子（无多对多授权） |
| **CHECK 约束** | `grade` 1~12；`gender` 0/1（NULL 允许）；`status` 三态（1/2/3）——DB 层兜底 |

---

## 九、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `exams` | `exams.kid_id → kids.id` | 试卷归属该孩子。`ON DELETE SET NULL`——删孩子不删试卷，试卷转为"未归属" |
| `questions` | 无 FK——作答者经 `exams.kid_id` JOIN 推导 | 该孩子做的题（`questions` 不存 `kid_id`） |
| `levels` | `levels.kid_id → kids.id` | 该孩子的知识点掌握水平。`ON DELETE CASCADE` |
| `gaps` | `gaps.kid_id → kids.id` | 该孩子的诊断结果（薄弱点清单）。`ON DELETE CASCADE` |
| `messages` | `messages.kid_id → kids.id` | 孩子通知。`ON DELETE CASCADE`；家长通知（`kid_id=NULL`）保留 |
| `tasks` | 无外键（Payload-Only，通过 `detail.kid_id` 关联） | 年级推进任务（`kind=2`）；删孩子不主动清，Handler 查不到 `kids` → `status=4(skipped)` |

**删除级联**（由 DB 外键自动生效，非应用层事务）：
```
删除 kids 行 →
  SET NULL（试卷数据全部保留，转为"未归属"）：
    exams（该孩子的试卷）
    → 其 pages / questions / question_kps 随 exams 一并存续
  CASCADE（随孩子一起清）：
    levels（知识点掌握水平）
    gaps（薄弱点清单）
    messages（该孩子的通知；家长通知 kid_id=NULL 保留）
  不动：
    tasks（Payload-Only——Worker 消费时 Handler 查不到 kids → status=4 skipped）
```

---

## 十、安全要点

| 项 | 措施 |
|----|------|
| `access_code` 熵 | ≈ 40 bit（31^8 ≈ 39.6 bit） |
| `access_code` 字符集 | `[2-9a-hj-km-np-z]`（31 字符）——排除 `0/o/1/l/i` |
| 生成冲突 | UNIQUE 约束 + 重试 ≤ 10 次 |
| PIN 存储 | bcrypt cost ≥ 12 |
| PIN 规则 | 4~6 位数字（若设置） |
| 学生端 JWT | 载荷含 `sub` / `typ=kid` / `pv` / `exp` |
| 主动失效 | `pin_version` |
| `access_code` 重置 | 生成新码，旧码立即失效 |
| 软删不可见 | `status='deleted'` 在任何查询中被过滤 |
| `access_code` 约束 | 字符集靠生成侧保证（Go 代码）；DB 不建 CHECK（SQLite 字符串正则约束可读性差） |

---

## 十一、索引

```sql
CREATE INDEX idx_kids_grade ON kids(grade);
CREATE UNIQUE INDEX idx_kids_name ON kids(name);
```

- `access_code` 的 UNIQUE 约束已隐含索引（用于学生端入口查询）
- `grade` 索引用于"按年级筛选"
- `idx_kids_name` 唯一索引保证姓名不重复（全表，含已删除/已毕业）

---


# `exams` 表设计（试卷实体）

## 一、业务用途

管理上传的试卷原件的元信息：学科、年级、考试类型、考试日期、归属孩子、上传者类型、总结描述。

**核心场景**：
- 家长或孩子上传试卷图片 → 创建 `exams` 行
- 试卷按学科 / 年级 / 考试类型 / 日期 分类
- 试卷归属某个孩子（`kid_id`）
- 记录"谁上传的"（家长 / 孩子）
- 后续 OCR 阶段：`questions.exam_id` 关联本表

**边界**：
- 试卷实体与"作答事实"解耦（作答走 `questions`）
- 无"总分"字段（不聚合、不记录总分）
- 无"版本树"（不做修正版映射——OCR 是机器识别，允许出错）
- 无 OCR / 切分状态字段（OCR 阶段再评估）
- 无标签关联（卷级维度已在 `exams` 表内；知识点标签打在题上）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 试卷唯一主键 |
| `title` | `TEXT` | NULL | NULL | 试卷标题（如"2026 深圳一模数学"）。家长可手填 |
| `subject_id` | `INTEGER` | NULL, FK → `subjects.id` | NULL | 学科（外键）。`ON DELETE SET NULL`。**试卷学科的权威源**（见第四章"枚举值定义"中"学科归属与筛选（分工）"段） |
| `grade` | `INTEGER` | NULL | NULL | 适用年级（1~12，与 `kids.grade` 同规范） |
| `exam_type` | `INTEGER` | NULL | NULL | 考试类型（代码枚举）：1=周测 2=月考 3=期中 4=期末 5=模考 6=专项 99=其他 |
| `exam_date` | `DATE` | NULL | NULL | 试卷考试日期 |
| `kid_id` | `INTEGER` | NULL, FK → `kids.id` | NULL | 归属孩子（外键）。`ON DELETE SET NULL`。NULL = 未归属 |
| `uploaded_by` | `INTEGER` | NOT NULL | 无 | 上传者类型（代码枚举）：1=家长 2=孩子 |
| `summary` | `TEXT` | NULL | NULL | 试卷总结（家长手填 + 未来 AI 生成） |
| `summary_source` | `INTEGER` | NULL | NULL | 1=manual（手工输入）2=ai（AI 生成） |
| `status` | `INTEGER` | NOT NULL | `1` | 状态（代码枚举）：1=active 2=deleted（软删） |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 创建时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 最后更新时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE exams (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    title          TEXT,
    subject_id     INTEGER REFERENCES subjects(id) ON DELETE SET NULL,
    grade          INTEGER,
    exam_type      INTEGER,
    exam_date      DATE,
    kid_id         INTEGER REFERENCES kids(id) ON DELETE SET NULL,
    uploaded_by    INTEGER NOT NULL,
    summary        TEXT,
    summary_source INTEGER,
    status         INTEGER NOT NULL DEFAULT 1,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (grade IS NULL OR grade BETWEEN 1 AND 12)
);

CREATE INDEX idx_exams_kid ON exams(kid_id);
CREATE INDEX idx_exams_subject ON exams(subject_id);
CREATE INDEX idx_exams_grade ON exams(grade);
CREATE INDEX idx_exams_exam_type ON exams(exam_type);
CREATE INDEX idx_exams_exam_date ON exams(exam_date);
```

---

## 四、枚举值定义（Go 侧）

### `exam_type`

| 值 | 常量 | 显示 |
|----|------|------|
| 1 | `ExamTypeWeekly` | 周测 |
| 2 | `ExamTypeMonthly` | 月考 |
| 3 | `ExamTypeMidterm` | 期中 |
| 4 | `ExamTypeFinal` | 期末 |
| 5 | `ExamTypeMock` | 模考 |
| 6 | `ExamTypeSpecial` | 专项 |
| 99 | `ExamTypeOther` | 其他 |

> **学科归属与筛选（分工）**：
> - **试卷列表筛选**以 `exam.subject_id` 为准（上传时 select 控件产出）。
> - **知识树 / 掌握度归类**以 `knowledge_points.subject_id` 为准（KP 自带学科）——`levels` / `gaps` 无 `exams` 直接关联，**当前不改查询**。
> - **一致性**：若某题的 KP 学科与试卷学科不符，**UI 在审核阶段提示**"该题知识点学科与试卷学科不符，请审核"——靠人工兜底，不加 DB 约束。

### `uploaded_by`

| 值 | 常量 | 显示 |
|----|------|------|
| 1 | `UploadedByParent` | 家长 |
| 2 | `UploadedByKid` | 孩子 |

### `status`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `ExamStatusActive` | 正常 |
| 2 | `ExamStatusDeleted` | 软删 |

### `summary_source`
| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `SummarySourceManual` | 手工输入 |
| 2 | `SummarySourceAI` | AI 生成 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层（`Valid()`）。

---

## 五、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 被 `pages.exam_id` / `questions.exam_id`（OCR 阶段）引用 |
| `title` | 列表显示；详情页标题；导出文件名 |
| `subject_id` | 列表筛选；JOIN `subjects` 显示学科名；OCR 阶段辅助判断 |
| `grade` | 列表筛选 |
| `exam_type` | 列表筛选；显示"模考"标签 |
| `exam_date` | 列表排序；"何时考的"信息 |
| `kid_id` | **"该生试卷"查询** = `WHERE kid_id = ?`；学生详情页跳转筛选 |
| `uploaded_by` | 详情页显示"由家长上传 / 由孩子上传"；**不记录具体哪个家长** |
| `summary` | 详情页展示；家长手填 + 未来 AI 生成 |
| `summary_source` | 展示摘要是如何生成的 |
| `status` | `1` 列表可见；`2` 软删（从列表消失，DB 保留） |
| `created_at` / `updated_at` | 审计 |

---

## 六、应用流程

### 6.1 上传试卷

**上传流程见 `pages` 表设计 6.1 节**（一次表单提交：元数据 + N 张图；三段式：先建 exam 行拿 id → 事务外落盘 → 建 pages 行）。

**上传者判定**：
- 家长端上传 → `uploaded_by=1`（parent）
- 孩子端上传 → `uploaded_by=2`（kid）；`kid_id` 强制为该孩子

**归属判定**：
- 家长端上传 → `kid_id` 由家长选择（可空）
- 孩子端上传 → `kid_id` 强制为该孩子

### 6.2 列表查询

```
GET /api/exams?subject_id=2&grade=9&exam_type=5&kid_id=1&cursor=0&limit=20

服务端（keyset 游标分页，非 OFFSET）：
SELECT e.*, s.name AS subject_name
FROM exams e
LEFT JOIN subjects s ON s.id = e.subject_id
WHERE e.status != 2                      -- 默认排除软删
  AND e.subject_id = ?
  AND e.grade = ?
  AND e.exam_type = ?
  AND e.kid_id = ?
  AND e.id < ?                           -- cursor>0 时；0/省略不加此条件
  -- UI 兜底：隐藏"无 pages 行"的孤儿 exam（上传中间态）
  AND EXISTS (SELECT 1 FROM pages p WHERE p.exam_id = e.id)
ORDER BY e.id DESC                       -- 与游标列一致，保证 keyset 稳定
LIMIT ?                                  -- limit+1，多取一条探测 has_more
```

> **游标说明**：分页用 **keyset 游标**（`cursor`=上一页最后一条 id，`limit`=单页条数）。
> 原因：OFFSET 在"边翻边删/增"时会漏行或重复行（位置切分受前面行增删影响）；`id` 单调自增，`id < cursor` 天然稳定。
> 排序列必须是游标列（`id`），否则游标不单调。响应返回 `next_cursor`（0=没有更多）与 `has_more`，**不返回 total**——
> 游标分页不提供精确总数（要总数就得每次 COUNT，违背 keyset 的初衷）。

> **`EXISTS` 兜底说明**：上传三段式中"阶段 1 后、阶段 3a 前"会有"有 exam 行、无 pages 行"的中间态；若此时崩溃，该行永无 pages。列表用 `EXISTS` 隐藏它，避免用户点进详情看到空卷。

### 6.3 详情查询

```
GET /api/exams/:id

服务端：
1. SELECT * FROM exams WHERE id = ? AND status = 1
2. SELECT * FROM pages WHERE exam_id = ? ORDER BY page_index
返回组合结果
```

### 6.4 编辑

```
PUT /api/exams/:id
{
  title, subject_id, grade, exam_type, exam_date,
  kid_id, summary
}

服务端：
UPDATE exams SET
  title=?, subject_id=?, grade=?, exam_type=?, exam_date=?,
  kid_id=?, summary=?, updated_at=CURRENT_TIMESTAMP
WHERE id = ?
```

### 6.5 删除（软删）

```
DELETE /api/exams/:id

服务端：
UPDATE exams SET status=2, updated_at=CURRENT_TIMESTAMP WHERE id = ?
（`pages` 保留——硬删时再级联清）
```

**恢复**：家长端"回收站"列出 `status=2` 的卷，可一键恢复（见 6.7 节）。

### 6.6 硬删（真删）——两阶段删除

**阶段一：事务内（秒级响应）**

1. BEGIN TRANSACTION
2. 向 `tasks` 写入文件清理任务：
   INSERT INTO tasks (kind, priority, status, detail)
   VALUES (4, 80, 0, json_object('dir', 'assets/exams/' || ?));
   -- kind=4: cleanup_storage
3. DELETE FROM exams WHERE id = ?;
   -- 外键 ON DELETE CASCADE 自动清 pages / questions
4. COMMIT
5. API 立即返回"删除成功"

**阶段二：异步 Worker 清理（不阻塞主线程）**

1. Worker 拉取 kind=4, status=0 的任务
2. `os.RemoveAll(payload.Dir)`（`assets/exams/14/` 整个目录）
3. 成功 → `tasks.status=2`（done）
4. 失败（文件占用/权限）→ 按 `retry_count` 重试
5. 通过异步重试，彻底避免孤儿文件长期残留

**安全要点**：
- `detail.dir` 由 Go 层用 `filepath.Join(baseDir, "exams", strconv.Itoa(examID))` 生成，**校验前缀**为 `assets/exams/`，防路径穿越
- 不裸拼 SQL 字符串（示例中的 `|| ?` 仅示意，实际由 Go 构造 JSON 入参）

### 6.7 回收站（软删恢复）

**列出软删卷**：
```
GET /api/exams/trash?page=1&page_size=20

SELECT e.* FROM exams e
WHERE e.status = 2
ORDER BY e.updated_at DESC    -- 软删时间倒序
LIMIT ? OFFSET ?
```

> 不带 `EXISTS (pages)` 兜底——孤儿 exam（上传中间态）是 `status=1`，不出现于此。

**恢复**：
```
POST /api/exams/:id/restore

UPDATE exams SET status=1, updated_at=CURRENT_TIMESTAMP
WHERE id=? AND status=2
```

**永久删除**：复用 6.6 节硬删（两阶段）。

**不做自动清理**：软删的卷文件仍在磁盘（6.5 节只改状态）；家庭场景磁盘足够，由家长手动永久删除。**不做"30 天自动硬删"**——避免家长想恢复时已被清。


---

## 七、设计决策记录

| 决策 | 理由 |
|------|------|
| **`subject_id` 外键** | 学科走 `subjects` 表（用户可编辑）；无冗余字符串 |
| **`grade` 用 INTEGER（1~12）** | 与 `kids.grade` 一致；排序自然 |
| **`exam_type` 存 INTEGER** | 代码枚举；紧凑、无歧义、演进友好 |
| **`kid_id` 可空** | 家长代传时可能暂未指定；可后续指定 |
| **`uploaded_by` 仅类型（parent/kid）** | 家庭内家长彼此可见；不需记具体哪个家长 |
| **`status` 仅 active/deleted** | 试卷无毕业语义；软删可恢复 |
| **无 `sha256` 字段** | 文件哈希由 `pages.sha256` 记录；试卷实体本身不存哈希 |
| **无 `total_score`** | 不聚合、不记录总分（用户明确） |
| **无 OCR 状态字段** | OCR 阶段再评估 |
| **无标签关联** | 卷级维度已在 `exams` 表内；知识点标签打在题上 |
| **`summary` 替代 `manual_notes`** | 统一"描述"职责；家长手填 + 未来 AI |
| **`ON DELETE SET NULL`** | `subject_id` / `kid_id` 被删时置 NULL，避免级联删试卷 |

---

## 八、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `subjects` | `exams.subject_id → subjects.id` | 学科（`ON DELETE SET NULL`） |
| `kids` | `exams.kid_id → kids.id` | 归属孩子（`ON DELETE SET NULL`） |
| `pages` | `pages.exam_id → exams.id` | 试卷页（1:N） |
| `questions`（OCR 阶段） | `questions.exam_id → exams.id` | 切出的题 |

**删除级联**（硬删走两阶段，见 6.6 节）：
```
硬删 exams 行 → 两阶段：
  阶段一（事务内）：
    → INSERT tasks(kind=4, detail={"dir":"assets/exams/{id}"})
    → DELETE exams WHERE id = ?（CASCADE 清 pages / questions）
  阶段二（异步 Worker）：
    → os.RemoveAll("assets/exams/{id}/")
```

**软删**：仅 `UPDATE exams SET status=2`——子表保留，恢复时一并回归。

---

## 九、索引

```sql
CREATE INDEX idx_exams_kid ON exams(kid_id);
CREATE INDEX idx_exams_subject ON exams(subject_id);
CREATE INDEX idx_exams_grade ON exams(grade);
CREATE INDEX idx_exams_exam_type ON exams(exam_type);
CREATE INDEX idx_exams_exam_date ON exams(exam_date);
```

**索引策略**：
- 单列索引服务"按字段筛选"
- 组合筛选（如 `subject_id + grade + kid_id`）——SQLite 自动选最优索引；若慢再考虑组合索引
- `status` 基数极低（3 态），不建独立索引

---


# `pages` 表设计（试卷页）

## 一、业务用途

存储一份试卷的每一页图片（1..N 张）。试卷是"多页"实体——拍照上传或扫描的每一页独立成行。

**核心场景**：
- 上传试卷时：一次上传 N 张图片 → 创建 N 条 `pages` 行
- 详情页按 `page_index` 顺序展示原图
- 后续 OCR 阶段：`questions.page_id` 关联本表（题目来自哪一页）
- 硬删试卷时：级联删除本表所有行 + 文件

**边界**：
- `pages` 只存"页"的元信息（路径 + 顺序 + 哈希）—— 不存 OCR 文本、不存切分结果
- 无"页级"标签（标签在 `exam` 或 `question` 层级）
- 无"页级"状态（页依附于 `exam` —— `exam` 软删即页不可见）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 试卷页唯一主键 |
| `exam_id` | `INTEGER` | NOT NULL, FK → `exams.id` | 无 | 所属试卷。`ON DELETE CASCADE` |
| `page_index` | `INTEGER` | NOT NULL | 无 | 页序（从 1 开始）。UI 按此排序展示 |
| `file_path` | `TEXT` | NOT NULL | 无 | 图片文件卷内相对路径（`assets/exams/{exam_id}/page_{index}.{ext}`） |
| `sha256` | `TEXT` | NOT NULL | 无 | 完整 SHA256（64 位 hex），仅用于文件完整性校验，不再承担跨卷去重职责 |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 创建时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE pages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    exam_id     INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    page_index  INTEGER NOT NULL,
    file_path   TEXT NOT NULL,
    sha256      TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exam_id, page_index)
);

CREATE INDEX idx_pages_exam ON pages(exam_id);
-- 注：无 sha256 索引（去重职责废弃，见第七章决策表）
```

---

## 四、文件系统设计

### 4.1 路径规则（卷内绑定）

```
assets/exams/{exam_id}/page_{page_index:03d}.{ext}
```

**规则**：
- **目录**：按试卷实体隔离——`assets/exams/{exam_id}/`
- **文件名**：`page_001.webp` / `page_002.webp`（页序补零 3 位）
- **扩展名**：不保留原图格式，自动转化成 `.webp`，参数用户可以选择

**示例**：
```
assets/exams/14/page_001.webp
assets/exams/14/page_002.webp
assets/exams/15/page_001.png
```

### 4.2 布局

完整布局见第二章 2.1 节（卷内绑定：页图 + 题图随卷存放，删卷即删目录）。

### 4.3 设计权衡（废弃全局引用计数）

| 项 | 选择 | 理由 |
|----|------|------|
| **卷内绑定目录** | ✅ | 删卷即删目录，**无需跨卷引用计数** |
| **不再全局 sha256 去重** | ✅ | 单机家庭场景省下的空间（~50GB）不值维护成本 |
| **不再 `assets/pages/{sha256:2}/` 分片** | ✅ | 目录数随试卷数增长，但单家庭试卷数百份，可控 |
| **DB 不再存 `sha256` 用于去重** | ⚠️ 保留列但仅作完整性校验 | 去重职责移除，`sha256` 降级为"文件完整性校验" |
| **`ON DELETE CASCADE` + 异步清理目录** | ✅ | 见第六章删除流程 |

> **为什么是卷内绑定而非全局去重**：全局 sha256 去重 + 引用计数删除在 SQLite 与文件系统异构存储下，事务无法覆盖磁盘 I/O，并发删除/回滚时易**误删其他试卷依赖的文件**或**残留永久孤儿文件**。卷内强绑定 + 异步清理牺牲少量磁盘空间（不再跨卷去重），换取删除路径的**确定性**。


---

## 五、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 被 `questions.page_id`（OCR 阶段）引用 |
| `exam_id` | 关联所属试卷；硬删试卷时级联删除 |
| `page_index` | UI 按序展示原图；OCR 阶段按页切题 |
| `file_path` | 静态资源访问；软删时保留文件；硬删时随卷目录整体清理 |
| `sha256` | **仅完整性校验**（不再去重、不再引用计数） |
| `created_at` | 审计 |

---

## 六、应用流程

### 6.1 上传试卷时创建（一次表单提交）

用户在**上传页面一次性提交**：元数据（学科/年级/考试类型/日期/归属/上传者）+ N 张图片。

**后端处理（三段式：先建行拿 id → 事务外落盘 → 再建页行）**：

**阶段 1：建 exam 行（独立事务，秒级）**
1. BEGIN
2. INSERT exams(...) → exam_id
3. COMMIT

**阶段 2：落盘（事务外，可能慢，不持锁）**
4. dir = "assets/exams/{exam_id}/"
5. MkdirAll(dir)
6. 对每张图片：
   a. 计算完整 SHA256（仅作完整性记录）
   b. filePath = dir + "page_{index:03d}.{ext}"
   c. os.WriteFile(filePath, ...)   ← 不在事务内

**阶段 3a：成功 → 建 pages 行（独立事务）**
7. BEGIN
8. INSERT pages(exam_id, page_index, file_path, sha256) × N
9. COMMIT
10. 返回 exam_id

**阶段 3b：失败 → 清理**
11. RemoveAll(dir)                     ← 尽力清理已落盘文件
12. DELETE exams WHERE id = exam_id    ← 删掉阶段 1 建的空 exam 行
13. 返回错误

**残留分析**（异构存储无法原子，靠"清理 + UI 过滤 + 任务扫尾"兜底）：

| 崩溃点 | DB | 文件 | 残留 |
|--------|-----|------|------|
| 阶段 1 后 | exam 行（无 pages） | 无 | 孤儿 exam 行 |
| 阶段 2 中/后 | exam 行（无 pages） | 部分/全部 | 孤儿 exam 行 + 孤儿文件 |
| 阶段 3a 后 | exam + pages | 全部 | 一致 ✓ |

- **正常失败**：阶段 3b 清理，无残留。
- **崩溃残留**：未来加 `cleanup_orphan_exams` 任务扫除。
- **UI 兜底**：`exams` 列表查询加 `EXISTS (SELECT 1 FROM pages WHERE exam_id=e.id)`，把"无 pages 的 exam"对用户隐藏。

> **为何不在事务内写文件**：文件 I/O 慢（N 张图可能数秒），放进事务会让事务长期持锁，写池串行下阻塞所有其他写操作。故"落盘"放事务外，代价是产生"孤儿 exam 行"中间态，由清理任务 + UI 过滤兜底。

### 6.2 详情展示

```
GET /api/exams/:id

服务端：
SELECT * FROM pages WHERE exam_id = ? ORDER BY page_index
```

**UI**：
```
[第 1 页] [第 2 页] [第 3 页] ...  ← 缩略图切换
或
纵向滚动查看全部
```

### 6.3 编辑（重排 / 增删页）

**方案 A**：直接增删 `pages` 行

```sql
DELETE FROM pages WHERE id = ?;
INSERT INTO pages(exam_id, page_index, file_path, sha256) VALUES (?, ?, ?, ?);
UPDATE pages SET page_index = ? WHERE id = ?;
```

**方案 B**：重新上传整份试卷

**推荐 A** —— 家庭场景，增删单页比重新上传省事。

**注意**：`UNIQUE(exam_id, page_index)` 保证页序无冲突。

**重排注意**：交换两页页序（如 1↔2）会瞬时撞 `UNIQUE(exam_id, page_index)`。应**先偏移再回写**（`UPDATE ... SET page_index = page_index + 1000 WHERE exam_id=?`，再按目标顺序回写），或按目标顺序**单列递减更新**。

**落地（请求内改 DB，Worker 只动磁盘）**：

- **接口**：`POST /api/exams/:id/pages/ops`（parent OR kid 归属校验），multipart
  带 `operations`（JSON：`delete_page_ids` + `final_order`——存量页数字 id、新页
  `new_<index>` 占位，index 恰为 0..新增数-1）+ 按序 `file` 字段（可空）；
  校验：final_order 必须是「存量页 ∪ new_N」的完整无重复全排列、总数 1..50、
  被删页不得出现在 final_order。响应 **202** `{task_id, exam_id}`。
- **DB**：`PageRepository.RebuildPages` 单事务 = 删行 → `page_index+1000` 偏移
  → 按目标序回写（规避 UNIQUE）→ 同事务入队 `tasks(kind=5, priority=60)`。
  新页行**不在请求里插**。
- **Worker**（`handleReorderPages`，幂等分阶段）：删被删页文件 → 存量页搬进
  `task_dir` 暂存（阶段 2 完成写 `phase2.done` marker）→ 新页与暂存文件落到
  `page_%03d.{ext}` 终名 → `InsertIfAbsent` 补插新页行 → 清暂存目录。
  **行在文件就位后才出现**，详情页图不会 404；marker 判重跑（目录不存在 =
  上轮已完成），阶段 6 清理失败按不可重试处理。
- **前端**：`ExamPagesEditor.vue`（原生 HTML5 拖排 + 上/下移按钮兜底触控）；
  202 后乐观重绘，轮询 `GET /api/exams/:id` 直到详情匹配目标序（`/api/tasks`
  是 requireParent，两端统一走详情轮询；60s 超时回滚，家长端补查任务错误）。

### 6.4 硬删

硬删的完整流程见 `exams` 表设计 6.6 节「硬删（真删）——两阶段删除」。

要点：
- 事务内：入队 `tasks(kind=4, detail={"dir":"assets/exams/{exam_id}"})` + `DELETE exams`
 - 异步：Worker `RemoveAll` 整个 `assets/exams/{exam_id}/` 目录
 - 不查 `sha256` 引用计数（去重职责废弃，见第七章决策表）

---

## 七、设计决策记录

| 决策 | 理由 |
|------|------|
| **独立成表（不塞 `exams`）** | 一份试卷 1..N 页；`exams` 表不能存变长列表 |
| **`UNIQUE(exam_id, page_index)`** | 保证页序无冲突；UI 排序稳定 |
| **卷内绑定目录 `assets/exams/{exam_id}/`** | 删卷即删目录；无需跨卷引用计数 |
| **文件名 `page_{index:03d}.{ext}`** | 页序直观；与 `page_index` 对应 |
| **`sha256` 降级为完整性校验** | 不再承担跨卷去重；去重职责废弃 |
| **不做目录分片** | 单家庭试卷数百份，目录数可控 |
| **`ON DELETE CASCADE`** | 硬删试卷 → 页自动清；应用层负责文件清理 |
| **无 `width` / `height` / `size`** | 派生自图片；不用存 DB |
| **无 OCR 状态字段** | OCR 阶段再评估（`questions` 表承接） |
| **无 `status`** | 页依附于 exam；exam 软删即页不可见 |
| **无标签关联** | 标签在 exam / question 层级，页级无标签 |
| **不在事务内写文件** | 文件 I/O 慢；放事务外，避免长期持锁；残留靠清理 + UI 过滤兜底 |

---

## 八、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `exams` | `pages.exam_id → exams.id` | 所属试卷（`ON DELETE CASCADE`） |
| `questions`（OCR 阶段） | `questions.page_id → pages.id` | 该页切出的题 |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 软删 `exams`（`status=2`） | `pages` 保留（恢复时可用） |
| 硬删 `exams` | 两阶段：事务内 `DELETE`（CASCADE 清 pages）+ 入队 `cleanup_storage`；异步 `RemoveAll` 目录 |
| 硬删单个 `page` | 事务内 DELETE 该行 + 删除该页原图 **+ 删除该页所有题图**（`SELECT image_path FROM questions WHERE page_id=?`，逐个 `os.Remove`；`questions.page_id` 随 `ON DELETE SET NULL` 置空）；失败仅记日志 |

---

## 九、索引

```sql
CREATE INDEX idx_pages_exam ON pages(exam_id);
-- 无 sha256 索引（去重职责废弃，见第七章决策表）
```

**索引策略**：
- `exam_id` 索引服务"查某试卷的所有页"
- `UNIQUE(exam_id, page_index)` 隐含组合索引——服务"按页序查询"

---

## 十、文件系统布局总览

完整布局见第二章 2.1 节。

---


# `subjects` 表设计（学科字典）

## 一、业务用途

管理学科字典：覆盖义务教育至高中全学科，支持家长自定义增删。

**核心场景**：
- 系统预置学科（语文、数学、英语、物理、化学、道德与法治、历史、地理、生物、科学、体育、艺术）
- 家长可新增自定义学科（如"物理竞赛"、"书法"）
- 家长可改名（如"道德与法治"→"道法"）
- 家长可调整显示顺序
- `exams.subject_id` / `knowledge_points.subject_id` 引用本表

**边界**：
- **不是枚举**（不像 `exam_type` / `status`）—— 学科是**可编辑数据**
- **不存年级维度**（年级独立，见 `kids.grade` / `exams.grade`）
- **不涉及题型**（题型见代码枚举）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 学科唯一主键 |
| `code` | `TEXT` | NOT NULL, UNIQUE | 无 | 英文代码（如 `math` / `chinese`），稳定不变，用于程序引用 |
| `name` | `TEXT` | NOT NULL | 无 | 中文显示名（如"数学"）。家长可改 |
| `sort_order` | `INTEGER` | NOT NULL | `0` | 展示顺序（数值越小越靠前） |
| `source` | `INTEGER` | NOT NULL | `1` | 来源：1=system（系统预置）2=user（用户自定义） |
| `mask` | `INTEGER` | NOT NULL | `31` | **可见位掩码**（5 bit）：低 3 位=学段（1=小学 2=初中 4=高中），高 2 位=文理（8=文科 16=理科），按位或。默认 `31`（全学段 + 全文理）。用于按孩子年级 + 文理过滤学科（上传页 / 雷达图）；家长可在科目管理中修改 |
| `mandatory` | `INTEGER` | NOT NULL | `0` | 必选标记：`1`=孩子不可取消（语文 / 数学）。孩子科目编辑强制勾选，服务端强制并入缺失的必选科（创建 / 修改孩子均不因此失败） |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 创建时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 最后更新时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE subjects (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  code           TEXT NOT NULL UNIQUE,
  name           TEXT NOT NULL,
  sort_order     INTEGER NOT NULL DEFAULT 0,
  source         INTEGER NOT NULL DEFAULT 1,
  mask           INTEGER NOT NULL DEFAULT 31,
  mandatory      INTEGER NOT NULL DEFAULT 0,
  created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subjects_sort ON subjects(sort_order);
CREATE INDEX idx_subjects_source ON subjects(source);
```

---

## 四、枚举值定义（Go 侧）

### `source`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `SourceSystem` | 系统预置 |
| 2 | `SourceUser` | 用户自定义 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

**`mask`（可见位掩码 · 5 bit）**：

低 3 位=学段，高 2 位=文理分科（与年级一一对应的过滤维度）：

| 位 | 值 | 常量 | 含义 |
|----|----|------|------|
| bit0 | 1 | `StagePrimary` | 小学（年级 1–6） |
| bit1 | 2 | `StageJunior` | 初中（年级 7–9） |
| bit2 | 4 | `StageSenior` | 高中（年级 10–12） |
| bit3 | 8 | `TrackArts` | 文科 |
| bit4 | 16 | `TrackScience` | 理科 |
| — | 7 | `GradeMaskAll` | 全学段 |
| — | 24 | `TrackMaskAll` | 全文理（文 \| 理） |
| — | 31 | `MaskAll` = `MaskMax` | 全开（默认）；bit5+ 保留 |

- **判定**：学科对某 `grade + track` 孩子可见 ⟺ `(mask & StageBitForGrade(grade)) != 0` **且** `(mask & TrackBitFor(track)) != 0`（两维各自 AND，某维位为 0 则该维不过滤）。
- **`StageBitForGrade(grade)`**：1–6→1，7–9→2，10–12→4；越界返回 0（不过滤该维）。
- **`TrackBitFor(track)`**：`1`→8（文），`2`→16（理），其余（含 `0` 未分科）→ 0（不过滤该维）。
- **高一才生效（强制）**：**`grade < 10` 时 `track` 一律视为 0**（不按文理过滤）——文理分科只从高中起，初中及以下即使已存 track 也不会隐藏科目。后端在 `SubjectService.ListByGradeTrack` 强制；前端 `trackBitFor(track, grade)` 同规则。`PUT /api/kids/:id` 仍可保存 track，仅在年级 ≥ 10 后才参与过滤。
- **校验**：`mask` 取值 `0~31`，越界（含 bit5+）由 service 层拒绝（`ErrInvalidInput`）。`0` 表示任何维度都不显示（家长可据此隐藏学科）。
- 核心科目（语文 / 数学 / 英语等）预置 `mask=31`（含文、理两个位），对任一分科恒可见；纯文/理科目（如物理→理科位、历史→文科位）只带对应分科位，故按分科隐藏。bit5（原"综合"位）已废弃：核心科直接同时携带文科位与理科位。

---

## 五、预置数据（seed）

**系统预置学科**（`source=1`）：

| code | name | sort_order | mask | mandatory |
|------|------|-----------|------|-----------|
| chinese | 语文 | 10 | 31 | 1 |
| math | 数学 | 15 | 31 | 1 |
| english | 英语 | 20 | 31 | 0 |
| biology | 生物 | 25 | 22 | 0 |
| geography | 地理 | 30 | 14 | 0 |
| history | 历史 | 35 | 14 | 0 |
| politics | 道法 | 40 | 31 | 0 |
| physics | 物理 | 45 | 22 | 0 |
| chemistry | 化学 | 50 | 22 | 0 |
| science | 科学 | 55 | 25 | 0 |
| art | 美术 | 60 | 31 | 0 |
| music | 音乐 | 65 | 31 | 0 |
| pe | 体育 | 70 | 31 | 0 |

**seed 导入策略**：
- **仅首次启动导入一次**；用 `cursors` 表标记：`task_name='seed_subjects'`，`last_status=2`（completed）后**不再导入**（防止家长删光预置学科后"表空 → 复活"）
- 幂等：按 `code` 跳过已存在
- 若家长已改名（如"道法"→"道德与法治"）—— 不覆盖（尊重用户修改）
- 若家长已删除某预置学科 —— 尊重删除（**不复活**）

---

## 六、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 被 `exams.subject_id` / `knowledge_points.subject_id` 引用 |
| `code` | 程序引用（如 `math`）；稳定不变；API 可作为参数传 |
| `name` | UI 显示；家长可改名 |
| `sort_order` | 学科列表顺序；UI 下拉排序 |
| `source` | UI 区分"系统预置"vs"自定义"；可限制"系统预置不允许删除"（或允许） |
| `mask` | 可见位掩码：上传页 / 雷达图按孩子年级 + 文理过滤学科；科目管理可编辑 |
| `mandatory` | 必选标记：孩子科目编辑强制勾选、服务端强制并入缺失的必选科（语文 / 数学） |
| `created_at` / `updated_at` | 审计 |

---

## 七、应用流程

### 7.1 首次启动导入

```
1. 查 cursors：SELECT last_status FROM cursors WHERE task_name='seed_subjects'
2. 若有记录且 last_status=2（completed）→ 跳过
3. 否则：
   a. 读 seed/subjects.json
   b. 批量 INSERT（source=1）；幂等按 code 跳过
   c. UPSERT cursors(task_name='seed_subjects', last_status=2, last_run_at=now)
4. 事务提交
```

**seed/subjects.json**（`mask` = 学段低 3 位 \| 文理高 2 位）：
```json
[
  {"code": "chinese", "name": "语文", "sort_order": 10, "mask": 31, "mandatory": 1},
  {"code": "math", "name": "数学", "sort_order": 15, "mask": 31, "mandatory": 1},
  {"code": "english", "name": "英语", "sort_order": 20, "mask": 31, "mandatory": 0},
  {"code": "biology", "name": "生物", "sort_order": 25, "mask": 22, "mandatory": 0},
  {"code": "geography", "name": "地理", "sort_order": 30, "mask": 14, "mandatory": 0},
  {"code": "history", "name": "历史", "sort_order": 35, "mask": 14, "mandatory": 0},
  {"code": "politics", "name": "道法", "sort_order": 40, "mask": 31, "mandatory": 0},
  {"code": "physics", "name": "物理", "sort_order": 45, "mask": 22, "mandatory": 0},
  {"code": "chemistry", "name": "化学", "sort_order": 50, "mask": 22, "mandatory": 0},
  {"code": "science", "name": "科学", "sort_order": 55, "mask": 25, "mandatory": 0},
  {"code": "art", "name": "美术", "sort_order": 60, "mask": 31, "mandatory": 0},
  {"code": "music", "name": "音乐", "sort_order": 65, "mask": 31, "mandatory": 0},
  {"code": "pe", "name": "体育", "sort_order": 70, "mask": 31, "mandatory": 0}
]
```
> 核心科（语数英体美音 / 道法）=31（全学段 + 文|理）；理科学科（物理/化学/生物）=22（初高 + 理科位）；
> 文科学科（历史/地理）=14（初高 + 文科位）；科学=25（小学 + 文|理）。
> 语文 / 数学 `mandatory=1`（孩子不可取消），其余为 0。

### 7.2 列出学科

```
GET /api/subjects                # 全量字典
GET /api/subjects?grade=9        # 按年级过滤（学段位）
GET /api/subjects?grade=10&track=1  # 年级 + 文理双过滤（grade<10 时 track 被强制忽略）
GET /api/subjects?kid_id=1       # 孩子视角：只返回 kid_subjects 中该孩子的科目（无记录即空）

服务端：
SELECT id, code, name, sort_order, source, mask, mandatory, created_at, updated_at
FROM subjects
  WHERE (?stageBit = 0 OR mask & ?stageBit != 0)
    AND (?trackBit = 0 OR mask & ?trackBit != 0)
  ORDER BY sort_order ASC, id ASC
（grade / track 缺省或越界 → 对应维度不过滤，向后兼容；grade<10 时 trackBit 恒 0）
（kid_id 存在且该孩子有白名单 → WHERE id IN (kid_subjects.subject_id)）
```

### 7.3 新增自定义学科

```
POST /api/subjects
{code: "physics_olympiad", name: "物理竞赛", sort_order: 45}

服务端：
1. 校验 code 唯一
2. 校验 code 格式（小写字母 + 下划线）
3. INSERT subjects(code, name, sort_order, source=2, mask=31, mandatory=0)
4. 返回
```

### 7.4 改名 / 改可见范围

```
PUT /api/subjects/:id
{name: "道法"}          # 改名
{mask: 6}              # 改可见范围（0~31；低 3 位学段，高 2 位文理）
{name: "道法", mask: 6} # 两者可同时

服务端：
UPDATE subjects SET name=?, mask=?, updated_at=CURRENT_TIMESTAMP WHERE id = ?
（source 不变；code 不可改；字段均可选，未传不更新）
```

**规则**：
- `code` **不可改**（程序引用；改了会破坏外键语义）
- `name` **可改**（显示名）
- `sort_order` **可改**
- `mask` **可改**（可见位掩码 0~31；越界拒绝）
- `mandatory` **由 seed 决定，API 不可改**

### 7.5 删除

```
DELETE /api/subjects/:id

服务端（单事务）：
1. 检查是否被引用：
   SELECT COUNT(*) FROM exams WHERE subject_id = ?
   SELECT COUNT(*) FROM knowledge_points WHERE subject_id = ?
2. 若有引用：
   - 方案 A：拒绝删除（返回 400 + "该学科下还有试卷或知识点"）
   - 方案 B：级联置 NULL（exams）或级联删除（knowledge_points）
3. 无引用 → DELETE
```

**推荐方案 A** —— 拒绝删除有引用的学科，让家长先处理引用。

**若家长确要删除** → 先删相关试卷 / 知识点，再删学科。

**`source=1`（系统预置）**是否允许删除？
- **允许** —— 尊重家长选择
- **或不允许** —— 保留系统完整性

**推荐**：**允许删除** —— 家庭场景，家长说了算。

---

## 八、设计决策记录

| 决策 | 理由 |
|------|------|
| **独立成表（不是枚举）** | 学科可编辑（家长自定义、改名、排序） |
| **`code` 与 `name` 分离** | `code` 稳定（程序引用）；`name` 可改（显示） |
| **`code` UNIQUE** | 防止重复 |
| **`code` 不可改** | 外键引用 `id`，但程序可能用 `code`；改了破坏语义 |
| **`source` 用 INT 枚举** | 与 `exam_type` / `status` 一致（DB 存 INT） |
| **预置学科 seed 导入** | 幂等；不覆盖用户改名；尊重用户删除 |
| **允许删除系统预置** | 家庭场景，家长说了算 |
| **`sort_order` 而非 `priority`** | 纯展示顺序，无业务权重 |
| **`mask` 用 5 bit 位图** | 学科跨学段（如政治全学段、物理仅中学）+ 文理分科（文/理），位图一次表达，无需关联表；按位过滤 O(1) |
| **`mask` 由家长可改** | 学段划分因地区/教材而异，预置只是默认，家长可在科目管理覆盖 |
| **`mandatory` 必选标记** | 语文 / 数学为中考必考，孩子在 `kid_subjects` 中不可取消；由 seed 固定，API 不可改 |
| **`kid_subjects` 决定孩子的科目** | `mask` 是全局字典属性，`kid_subjects` 是"该生实际所学"的个体集合；孩子相关的科目查询一律只读 `kid_subjects`，**无 mask 回退**——无记录即"无科目" |

---

## 九、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `exams` | `exams.subject_id → subjects.id` | 试卷归属学科（`ON DELETE SET NULL`） |
| `knowledge_points`（知识点字典） | `knowledge_points.subject_id → subjects.id` | 知识点归属学科（`ON DELETE CASCADE`） |
| `kid_subjects`（孩子科目白名单） | `kid_subjects.subject_id → subjects.id` | 该生实际所学学科（`ON DELETE RESTRICT`） |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 删 `subjects` 行 | 若被 `kid_subjects` 引用 → 应用层拒绝（`ErrInvalidInput`）；否则按 `exams` `SET NULL` / `knowledge_points` `CASCADE` |

**推荐**：
- 应用层"先查引用、有则拒"
- DB 层 `ON DELETE SET NULL`（exams）/ `CASCADE`（knowledge_points）作为兜底

---

## 十、索引

```sql
CREATE INDEX idx_subjects_sort ON subjects(sort_order);
CREATE INDEX idx_subjects_source ON subjects(source);
```

**索引策略**：
- `sort_order` 索引服务"按顺序列出"
- `source` 索引服务"筛选系统预置 / 自定义"
- `code` 的 UNIQUE 约束隐含索引——服务"按 code 查询"

---


# `kid_subjects` 表设计（孩子科目白名单）

## 一、业务用途

记录**某个孩子实际学习的科目**（"孩子 × 学科"关联）。它是 `subjects.mask` 之外的个体维度：某省不考某科、家庭想隐藏某科时，在此配置即可，不影响全局字典。

**核心场景**：
- 家长为孩子勾选实际所学科目（如初中生不选物理）
- 上传页 / 雷达图 / 试卷分布 / 科目归档统计的科目一律取自 `kid_subjects`；**无记录即空**，不做 `subjects.mask` 回退。
- 语文 / 数学（`subjects.mandatory=1`）不可取消

## 二、字段定义

| 字段 | 类型 | 约束 | 默认 | 说明 |
|------|------|------|------|------|
| `kid_id` | `INTEGER` | NOT NULL, FK → `kids.id` | 无 | 孩子（`ON DELETE CASCADE`：删孩子清其白名单） |
| `subject_id` | `INTEGER` | NOT NULL, FK → `subjects.id` | 无 | 学科（`ON DELETE RESTRICT`：仍被引用的学科不可删） |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 创建时间 |

复合主键 `(kid_id, subject_id)`，天然去重。

## 三、建表 SQL

```sql
CREATE TABLE kid_subjects (
  kid_id       INTEGER NOT NULL REFERENCES kids(id) ON DELETE CASCADE,
  subject_id   INTEGER NOT NULL REFERENCES subjects(id) ON DELETE RESTRICT,
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (kid_id, subject_id)
);

CREATE INDEX idx_kid_subjects_subject ON kid_subjects(subject_id);
```

## 四、判定规则

- **白名单优先**：孩子存在 `kid_subjects` 行 ⟺ 以其为可见集合（忽略 `subjects.mask`）。
- **无记录即空**：孩子无任何行 ⟺ 该孩子无科目（不查 `subjects.mask`）。
- **必选约束**：`PUT /api/kids/:id/subjects`（及创建 / 修改孩子的原子写路径）会将所有 `subjects.mandatory=1` 的行强制并入集合，即使调用方未提供；服务端保证必选科永不缺失，而非拒绝请求。
- **消费方**（全部只读 `kid_subjects`，无 mask 回退）：
  - 雷达图科目轴 = `GET /api/subjects?kid_id=N`。
  - 试卷分布 = `GET /api/dashboard/distribution?kid_id=N`：只统计 `kid_subjects` 内科目的试卷（`JOIN kid_subjects`）。
  - 上传页科目下拉 = `GET /api/subjects?kid_id=N`。

## 五、应用流程

```
GET /api/kids/:id/subjects   # 返回该孩子 kid_subjects 展开后的学科；无记录返回空数组
PUT /api/kids/:id/subjects   # 全量替换 {subject_ids:[...]}；空数组 = 清空（该孩子无科目）
```

服务端（PUT，单事务）：
1. 校验孩子存在（否则 `ErrNotFound`）
2. 校验所有 `subject_ids` 均为真实学科（否则 `ErrInvalidInput`）
3. 校验必选科未遗漏（否则 `ErrInvalidInput`）
4. `DELETE FROM kid_subjects WHERE kid_id=?` → 逐行 `INSERT`

## 六、设计决策记录

| 决策 | 理由 |
|------|------|
| **独立关联表（非 `subjects.kids` 数组）** | 关系型外键保证引用完整性，过滤 O(1) |
| **`subject_id` `RESTRICT`** | 保护白名单：仍被孩子引用的学科不能删（应用层映射为 `ErrInvalidInput`） |
| **`kid_id` `CASCADE`** | 删孩子时自动清其白名单，无悬挂行 |
| **空 = 无科目（不回退）** | 未配置即"无科目"，查询直接返回空；不做 `mask` 回退，避免"没配却显示一堆科目"的错觉 |
| **必选科后端强制** | 仅前端 disabled 不够：直接调 API 也需拒绝，否则孩子可不显示语文/数学 |

## 七、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `kids` | `kid_subjects.kid_id → kids.id` | 归属孩子（`ON DELETE CASCADE`） |
| `subjects` | `kid_subjects.subject_id → subjects.id` | 所学学科（`ON DELETE RESTRICT`） |

## 八、索引

```sql
CREATE INDEX idx_kid_subjects_subject ON kid_subjects(subject_id);
```

- `(kid_id, subject_id)` 主键隐含索引——服务"按孩子取白名单"（最常用）
- `subject_id` 索引——服务"该学科是否仍被引用"（删除学科前的探测）

---


# `knowledge_points` 表设计（知识点字典）

## 一、业务用途

管理知识点字典：每个知识点归属某个学科，支持家长自定义增删。

**核心场景**：
- 系统预置知识点（如"数学 → 二次函数"、"物理 → 欧姆定律"）
- 家长可新增自定义知识点（如"数学 → 二次函数最值"）
- 家长可改名、调整显示顺序
- `question_kps.knowledge_point_id` 引用本表
- `levels.knowledge_point_id` 引用本表

**边界**：
- **两级固定**（学科 → 知识点）—— 由 `subject_id` 表达，不用 `parent_id`
- **不存年级**（年级独立）
- **不存难度**（难度在 `questions.difficulty`）
- **不涉及题型**（题型见代码枚举）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 知识点唯一主键 |
| `subject_id` | `INTEGER` | NOT NULL, FK → `subjects.id` | 无 | 所属学科。`ON DELETE CASCADE` |
| `name` | `TEXT` | NOT NULL | 无 | 知识点名称（如"二次函数"）。家长可改 |
| `sort_order` | `INTEGER` | NOT NULL | `0` | 展示顺序（数值越小越靠前） |
| `source` | `INTEGER` | NOT NULL | `1` | 来源：1=system（系统预置）2=user（用户自定义） |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 创建时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 最后更新时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE knowledge_points (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    subject_id  INTEGER NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    source      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(subject_id, name)
);

CREATE INDEX idx_kp_subject ON knowledge_points(subject_id);
CREATE INDEX idx_kp_sort ON knowledge_points(subject_id, sort_order);
CREATE INDEX idx_kp_source ON knowledge_points(source);
```

---

## 四、枚举值定义（Go 侧）

### `source`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `SourceSystem` | 系统预置 |
| 2 | `SourceUser` | 用户自定义 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

---

## 五、预置数据（seed）

**教研侧关系文件** —— 按学科分文件存放，随 seed 落盘到数据目录，**不入库**
（`knowledge_points` 目前无 seed 消费者；知识点由家长按需新增，`source=2`）。

**seed 文件结构**：`seed/knowledge_edges_{学科名}.json`（如 `knowledge_edges_数学.json`），
内容为前置关系边（`{parent, child, relation: prerequisite}`），供后续版本教研侧使用。

**预留游标名**：`seed_knowledge_points`（见 `cursors` 章），供未来导入使用。

---

## 六、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 被 `question_kps.knowledge_point_id` / `levels.knowledge_point_id` / `gaps.knowledge_point_id` 引用 |
| `subject_id` | 关联所属学科；UI 按学科分组展示知识点 |
| `name` | UI 显示；家长可改名 |
| `sort_order` | 知识点列表顺序；UI 下拉排序 |
| `source` | UI 区分"系统预置"vs"自定义" |
| `created_at` / `updated_at` | 审计 |

---

## 七、应用流程

### 7.1 首次启动导入

```
1. 检测 cursors 表 task_name='seed_knowledge_points'
2. 若 `last_status != 2`（未 completed）：
   a. 遍历 seed/knowledge_points_*.json
   b. 对每个学科文件：
      - 按 code 查该学科的 subject_id
      - 批量 INSERT knowledge_points(subject_id, name, sort_order, source=1)
   c. UPSERT cursors(task_name='seed_knowledge_points', last_status=2, last_run_at=now)
3. 事务提交
```

### 7.2 列出知识点

```
GET /api/knowledge-points?subject_id=2

服务端：
SELECT * FROM knowledge_points
WHERE subject_id = ?
ORDER BY sort_order ASC, id ASC
```

### 7.3 新增自定义知识点

```
POST /api/knowledge-points
{subject_id: 2, name: "二次函数最值", sort_order: 75}

服务端：
1. 校验 subject_id 存在
2. 校验 (subject_id, name) 唯一
3. INSERT knowledge_points(subject_id, name, sort_order, source=2)
4. 返回
```

### 7.4 改名

```
PUT /api/knowledge-points/:id
{name: "新名称"}

服务端：
UPDATE knowledge_points SET name=?, updated_at=CURRENT_TIMESTAMP WHERE id = ?
```

**规则**：
- `name` **可改**
- `subject_id` **可改**（移动到其他学科）
- `sort_order` **可改**

### 7.5 删除

```
DELETE /api/knowledge-points/:id

服务端（单事务）：
1. 检查是否被引用：
   SELECT COUNT(*) FROM question_kps WHERE knowledge_point_id = ?
   SELECT COUNT(*) FROM levels WHERE knowledge_point_id = ?
2. 若有引用 → 拒绝删除（返回 400 + "该知识点下还有题或掌握度记录"）
3. 无引用 → DELETE
```

**推荐**：**拒绝删除有引用的知识点**，让家长先处理引用。

---

## 八、设计决策记录

| 决策 | 理由 |
|------|------|
| **独立成表（不是枚举）** | 知识点可编辑（家长自定义、改名、排序） |
| **表名 `knowledge_points`（全称）** | 与 `subjects` 对齐 |
| **无 `code`** | 知识点数量 600~1200，起英文 code 不现实；靠 `id` 引用 |
| **`subject_id` 外键 + `CASCADE`** | 删学科 → 知识点自动清 |
| **`UNIQUE(subject_id, name)`** | 同学科下不重名 |
| **无 `parent_id`** | 两级固定（学科 → 知识点）；不再嵌套 |
| **`source` 用 INT 枚举** | 与 `subjects` 一致 |
| **seed 导入一次性** | 用 `cursors` 表记录已导入；避免"删了又复活" |
| **`sort_order` 而非 `priority`** | 纯展示顺序 |
| **允许删除系统预置** | 家庭场景，家长说了算 |

---

## 九、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `subjects` | `knowledge_points.subject_id → subjects.id` | 所属学科（`ON DELETE CASCADE`） |
| `question_kps`（OCR 阶段） | `question_kps.knowledge_point_id → knowledge_points.id` | 题 ↔ 知识点 |
| `levels`（OCR 阶段） | `levels.knowledge_point_id → knowledge_points.id` | 每孩每知识点水平 |
| `gaps`（OCR 阶段） | `gaps.knowledge_point_id → knowledge_points.id` | 诊断结果 |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 删 `subjects` 行 | `CASCADE` 删其所有 `knowledge_points` |
| 删 `knowledge_points` 行 | 应用层先查引用；有则拒；无则删 |

---

## 十、索引

```sql
CREATE INDEX idx_kp_subject ON knowledge_points(subject_id);
CREATE INDEX idx_kp_sort ON knowledge_points(subject_id, sort_order);
CREATE INDEX idx_kp_source ON knowledge_points(source);
```

**索引策略**：
- `subject_id` 索引服务"查某学科的所有知识点"
- `(subject_id, sort_order)` 组合索引服务"按学科 + 顺序列出"
- `source` 索引服务"筛选系统预置 / 自定义"

---



# `migrations` 表设计（迁移记录）

## 一、业务用途

自研迁移机制的元数据表：记录哪些迁移脚本已应用、何时应用。

**核心场景**：
- 应用启动时读取 `migrations/` 目录（embed 嵌入二进制）
- 对比 `migrations` 表记录 → 应用未执行的脚本
- 幂等：已应用的脚本不重复执行
- 体检页读取最新 `display_tag` 展示业务版本号

**边界**：
- **仅支持 up 迁移**（单方向）—— 无 down / 回滚
- **不记录 schema 快照** —— 只记录"迁移应用历史"
- **不是框架表** —— 自研逻辑显式写入（非 `golang-migrate` / `goose` 自动维护）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 迁移记录唯一主键 |
| `version` | `INTEGER` | NOT NULL, UNIQUE | 无 | 迁移脚本顺序号（从 1 起算；对应文件名前缀） |
| `name` | `TEXT` | NOT NULL | 无 | 迁移脚本文件名（如 `0001_init.sql`） |
| `display_tag` | `TEXT` | NULL | NULL | 业务文档对应版本号（如 `v1.0.0`），体检页展示用 |
| `applied_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 迁移应用时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE migrations (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    version      INTEGER NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    display_tag  TEXT,
    applied_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## 四、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 主键 |
| `version` | 迁移脚本顺序号；启动时对比"已应用 vs 待应用" |
| `name` | 审计；"哪个文件被应用了" |
| `display_tag` | 体检页读 `SELECT ... ORDER BY version DESC LIMIT 1` 显示；对应业务文档版本号 |
| `applied_at` | 审计；"何时升级的" |

---

## 五、应用流程

### 5.1 首次启动建表

```
1. 检查 migrations 表是否存在（sqlite_master 查询）
2. 不存在 → CREATE TABLE migrations (...)
3. 存在 → 跳过
（此步为整个迁移机制的引导——建表语句硬编码在 Go 代码中）
```

### 5.2 启动时迁移

```
1. 读 embed 目录 migrations/*.sql（按文件名排序）
2. 对每个脚本：
   a. 解析文件名 → version（如 0012 → 12）
   b. 查表：SELECT id FROM migrations WHERE version = 12
   c. 若已存在 → 跳过
   d. 若不存在：
      - BEGIN TRANSACTION
      - 执行脚本 SQL
      - INSERT migrations(version, name, display_tag)
      - COMMIT
3. 全部应用完成 → 启动服务
```

### 5.3 体检页读版本

取最新 `display_tag` 的口径见第四章功能表 `display_tag` 行；空表兼容兜底 SQL 见第二章 2.2 节。

---

## 六、设计决策记录

| 决策 | 理由 |
|------|------|
| **表名 `migrations`（非 `schema_version`）** | 语义更直接："迁移记录" |
| **自研而非用框架** | 迁移简单（单方向 up）；无回滚需求；零依赖 |
| **`version` UNIQUE** | 防止重复应用；启动对比用 |
| **`display_tag` 保留** | 体检页展示业务版本号；与迁移版本解耦 |
| **仅 up 迁移** | 无 down；无回滚 |
| **文件名前缀即版本号** | `0001_xxx.sql` → version=1；约定俗成 |
| **`name` 存文件名** | 便于审计"哪个脚本被应用了" |

---

## 七、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| — | — | `migrations` 是独立元数据表，不被其他表外键引用 |

---

## 八、索引

**无独立索引**——`UNIQUE(version)` 已隐含索引，服务"按版本查询"和"对比是否已应用"。

**索引策略**：
- 无其他查询模式

---


# `metrics` 表设计（运维指标日聚合）

## 一、业务用途

为单机环境提供轻量级可观测性指标存储：按"指标名 + 日期"聚合当日累计计数，支撑体检页近 7 天异常报警。

**核心场景**：
- 应用运行中打点（如 `parse_fail`、`backup_success`）
- 每日一行一指标（upsert）
- 体检页读"近 7 天某指标合计"
- 定期清理 90 天前的旧数据

**边界**：
- **不是日志表** —— 不记录"每次事件的明细"，只记录"当日累计数"
- **不是实时监控** —— 单机场景，无需 Prometheus / Grafana
- **不参与业务逻辑** —— 打点失败不阻断主流程

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `metric_name` | `TEXT` | NOT NULL | 无 | 指标名（如 `parse_fail` / `db_size_kb`） |
| `metric_date` | `DATETIME` | NOT NULL | 无 | 日期分区（ISO8601，本地日） |
| `count` | `INTEGER` | NOT NULL | `0` | counter=当日累计次数；gauge=当日取值快照 |
| `is_gauge` | `INTEGER` | NOT NULL | `0` | `0`=counter（累加） / `1`=gauge（覆盖） |
| `updated_at` | `DATETIME` | NOT NULL | 无 | 最后更新时间戳（ISO8601 字符串） |

**主键**：`(metric_name, metric_date)` 复合主键——天然去重。

---

## 三、建表 SQL

```sql
CREATE TABLE metrics (
    -- SQLite 无原生 DATE 类型；metric_date / updated_at 存 ISO8601，
    -- 与全库 DATETIME 约定一致（SQLite 内 DATETIME 即 TEXT 亲和性）。
    metric_name TEXT NOT NULL,
    metric_date DATETIME NOT NULL,
    count       INTEGER NOT NULL DEFAULT 0,
    is_gauge    INTEGER NOT NULL DEFAULT 0,   -- 0=counter(累加) 1=gauge(覆盖)
    updated_at  DATETIME NOT NULL,
    PRIMARY KEY (metric_name, metric_date)
);

CREATE INDEX idx_metrics_date ON metrics(metric_date);
```

---

## 四、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `metric_name` | 指标唯一标识；代码里预定义常量 |
| `metric_date` | 日聚合分区键；体检页按日期区间求和 |
| `count` | counter=当日累计；gauge=当日快照值 |
| `is_gauge` | 告诉读侧该 `SUM` 还是取快照（见第六章 `is_gauge` 语义契约） |
| `updated_at` | 审计；"该指标最后一次打点时间" |

---

## 五、预定义指标名

**代码侧维护指标名清单**（不建表）：

**counter（`is_gauge=0`，当日累加）**

| 指标名 | 打点时机 |
|--------|----------|
| `parse_fail` | JSON / 时间窗解析失败 |
| `backup_success` | 备份成功 |
| `backup_fail` | 备份失败 |
| `maintenance_success` | 定时任务完成 |
| `maintenance_fail` | 定时任务失败 |

**gauge（`is_gauge=1`，每日覆盖写当日快照）**

| 指标名 | 打点时机 |
|--------|----------|
| `db_size_kb` | 每日维护：`paperly.db` 主库文件大小 / 1024（不含 `-wal`/`-shm`） |
| `assets_size_kb` | 每日维护：遍历 `assets/` 求和 / 1024 |
| `fs_free_kb` | 每日维护：`Statfs(dataDir)` 的可用字节 / 1024 |
| `fs_total_kb` | 每日维护：`Statfs(dataDir)` 的总字节 / 1024 |

**未来按需扩展** —— 加指标不改表结构。

---

## 六、应用流程

### 6.1 `is_gauge` 语义契约

| `is_gauge` | 语义 | 写入 | 读取 |
|-----------|------|------|------|
| 0（counter） | 当日累计次数 | `count = count + 1` | `SUM(count)` |
| 1（gauge） | 当日取值快照 | `count = excluded.count` | `MAX(count)` 或读当日行 |

**显式存一列而不是靠读侧记住指标名**：读侧拿到一行就知道该 `SUM` 还是该取快照，
不必为每个指标名写分支；新增指标也不会因为忘了归类而算出「一天执行了 1 次备份」
这种静默错误的和。

### 6.2 打点（counter）

```sql
-- upsert 语义：当日 +1
INSERT INTO metrics (metric_name, metric_date, count, is_gauge, updated_at)
VALUES (?, date('now', 'localtime'), 1, 0, datetime('now', 'localtime'))
ON CONFLICT(metric_name, metric_date)
DO UPDATE SET count = count + 1, updated_at = excluded.updated_at;
```

**特点**：
- **单条 SQL 完成 upsert** —— 无竞态（写池单连接串行）
- **独立于业务事务** —— 打点失败不影响主流程
- **跨进程重启持久** —— 内存方案会丢

### 6.3 写快照（gauge）

```sql
-- 覆盖语义：同日重跑是覆盖而非累加
INSERT INTO metrics (metric_name, metric_date, count, is_gauge, updated_at)
VALUES (?, date('now','localtime'), ?, 1, datetime('now','localtime'))
ON CONFLICT(metric_name, metric_date)
DO UPDATE SET count = excluded.count, updated_at = excluded.updated_at;
```

注意 DO UPDATE 里 **不能写 `count = count + 1`**：每日维护重跑（手动触发、失败重试）
会把体量翻倍，图上出现毫无意义的尖峰。

### 6.4 读近 7 天合计

```sql
SELECT COALESCE(SUM(count), 0)
FROM metrics
WHERE metric_name = 'parse_fail'
  AND is_gauge = 0
  AND metric_date >= date('now', '-6 days', 'localtime');
```

**体检页展示**："近 7 天解析失败 N 次"——超阈值（如 > 20）标 ⚠️。

读体量趋势走 gauge 分支：`WHERE metric_name = 'db_size_kb' AND is_gauge = 1
AND metric_date >= date('now', '-29 days', 'localtime')`，按日期升序取 `count`。

### 6.5 清理

```sql
-- 每日维护时执行
DELETE FROM metrics WHERE metric_date < date('now','-90 day','localtime')  AND is_gauge = 0;
DELETE FROM metrics WHERE metric_date < date('now','-365 day','localtime') AND is_gauge = 1;
```

**保留策略分档**：counter 90 天（体检只需近 7 天，留 90 天足够回溯）；
gauge 365 天——体量是慢变量，90 天看不出增长斜率，而一年的日粒度也只有 365 行。

---

## 七、设计决策记录

| 决策 | 理由 |
|------|------|
| **表名 `metrics`（非 `system_metrics`）** | 简化；"system_" 前缀冗余 |
| **日聚合而非事件明细** | 体检只需"数量"；明细可由日志替代 |
| **复合主键 `(metric_name, metric_date)`** | 天然去重；无需 `id` 列 |
| **`metric_date` 用 DATETIME** | 与全库时间列类型统一；SQLite 内 DATETIME 即 TEXT 亲和性，`YYYY-MM-DD` 字符串可直接比较 / 排序 |
| **`updated_at` 用 DATETIME** | 同上；ISO8601 字符串 |
| **加 `is_gauge` 显式区分 counter/gauge** | 读侧不必记住指标名即可选对聚合方式；新增指标不会静默算错 |
| **保留期分档（counter 90 天 / gauge 365 天）** | 两者变化速率差一个数量级，统一保留期要么留垃圾要么丢趋势 |
| **`count` 累计** | 只增不减（除清理）；简单 |
| **打点独立于业务事务** | 打点失败不阻断主流程（家庭场景，观测数据可丢） |
| **90 天保留** | 体检只需 7 天；留 90 天足够回溯；清理控制表大小 |
| **指标名代码侧维护** | 不建枚举表；加指标不改 schema |

---

## 八、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| — | — | `metrics` 是独立观测表，不被其他表外键引用 |
| `cursors` | 无外键；通过"每日维护任务"关联 | 维护任务执行时顺带清理旧 `metrics` |

---

## 九、索引

```sql
CREATE INDEX idx_metrics_date ON metrics(metric_date);
```

**索引策略**：
- 复合主键 `(metric_name, metric_date)` 已覆盖"按指标 + 日期查询"
- 额外 `metric_date` 索引服务"按日期清理"（`DELETE WHERE metric_date < ?`）
- 无需其他索引

---


# `cursors` 表设计（任务游标）

## 一、业务用途

为可中断、可恢复的后台任务提供主键游标持久化：记录"已处理到哪"、"上次何时跑"、"当前状态"。

**核心场景**：
- 大批量扫描（如首次导入全学科知识点）分批处理
- 任务超时中断 → 下次启动从游标续跑，不从头开始
- Seed 导入标记（如 `seed_knowledge_points` 已导入 → 不再重复）
- 状态查询（运维可见"上次维护何时跑的"）

**边界**：
- **不是任务队列** —— 队列见 `tasks` 表
- **不是调度器** —— 触发由应用层（启动时 / 定时器 / 外部 cron）负责
- **不存业务数据** —— 只存"游标位置 + 状态"
- **断点续跑分支为未来预留，当前无消费者**（seed 走一次性标记、聚合/年级不走 cursors、清理一条 DELETE 即可）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 游标记录唯一主键 |
| `task_name` | `TEXT` | NOT NULL, UNIQUE | 无 | 任务唯一标识（如 `seed_subjects` / `seed_knowledge_points`） |
| `last_id` | `INTEGER` | NOT NULL | `0` | 上次处理完成的最大记录主键 ID（断点续跑用） |
| `last_run_at` | `DATETIME` | NULL | NULL | 最近一次执行启动时间 |
| `last_status` | `INTEGER` | NOT NULL | `0` | 状态枚举：0=pending 1=running 2=completed 3=failed |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 记录更新时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE cursors (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_name    TEXT NOT NULL UNIQUE,
    last_id      INTEGER NOT NULL DEFAULT 0,
    last_run_at  DATETIME,
    last_status  INTEGER NOT NULL DEFAULT 0,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

```

---

## 四、枚举值定义（Go 侧）

### `last_status`

| 值 | 常量 | 含义 |
|----|------|------|
| 0 | `CursorStatusPending` | 尚未启动 |
| 1 | `CursorStatusRunning` | 执行中 |
| 2 | `CursorStatusCompleted` | 已完成 |
| 3 | `CursorStatusFailed` | 失败 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

---

## 五、预定义任务名

**代码侧维护任务名清单**（不建表）：

| `task_name` | 用途 |
|-------------|------|
| `seed_subjects` | 首次导入学科（一次性标记） |
| `seed_knowledge_points` | 首次导入知识点（一次性标记） |

> **注**：年级推进走 `tasks.kind=2`；掌握度聚合走 `tasks.kind=1`（aggregate_gaps）——**不在 `cursors` 里**，`cursors` 只承载"断点续跑型"或"一次性标记型"任务。

**未来按需扩展** —— 加任务不改表结构。

---

## 六、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 主键 |
| `task_name` | 唯一标识任务；`UNIQUE` 保证同任务一行 |
| `last_id` | 断点续跑：下次从 `id > last_id` 继续 |
| `last_run_at` | 运维展示"上次何时跑"；判断是否超过调度周期 |
| `last_status` | 状态机；应用层判断是否可启动 |
| `updated_at` | 审计 |

---

## 七、应用流程

### 7.1 断点续跑模式（大任务分批）

**（未来预留分支，当前无消费者）**——seed 走 7.2 节一次性标记、年级走 `tasks.kind=2`、聚合/每日维护不走游标；本节描述保留供未来大任务分批使用。

> **启动复位**：进程启动时，若发现 `last_status=1(running)`（上次崩溃残留），视为可续跑——从 `last_id` 继续；无需手动改状态。

```
1. 读游标：SELECT last_id, last_status FROM cursors WHERE task_name = 'xxx'
2. 若 last_status = running：
   - 上次中断 → 从 last_id 续跑
3. 循环：
   a. SELECT * FROM 目标表 WHERE id > last_id ORDER BY id LIMIT 500
   b. 处理这一批
   c. UPDATE cursors SET last_id = 当前批最大 id, updated_at = now WHERE task_name = 'xxx'
   d. 若超时 / 中断 → 保留 last_id，标记 last_status=1（running）→ 下次续跑
   e. 若处理完 → UPDATE last_status=2（completed）
```

### 7.2 一次性标记模式（Seed 导入）

```
1. 读游标：SELECT last_status FROM cursors WHERE task_name = 'seed_subjects'
2. 若无记录：
   - 执行导入
   - INSERT cursors(task_name, last_status=2, last_run_at=now)
3. 若有记录且 last_status=2：
   - 跳过导入
```

**这解决**："Seed 只导入一次，后续启动不重复"。

### 7.3 状态查询（运维）

```sql
SELECT task_name, last_id, last_run_at, last_status
FROM cursors
ORDER BY task_name;
```

**体检页展示**："上次学科导入：2026-09-01 03:00（已完成）"

---

## 八、设计决策记录

| 决策 | 理由 |
|------|------|
| **表名 `cursors`（非 `maintenance_cursor`）** | 简化；"maintenance_" 前缀冗余 |
| **`task_name` UNIQUE** | 同任务一行；避免重复 |
| **`last_id` 主键游标（非 OFFSET）** | OFFSET 随偏移线性变慢；主键游标恒定快；且写入时行不移位 |
| **`last_status` 用 INT 枚举** | 与 `exam_type` / `status` 一致 |
| **`last_run_at` 可空** | 首次未跑过为 NULL |
| **支持"一次性标记"模式** | Seed 导入用 `last_status=2` 表示"已完成，不再执行" |
| **不与 `tasks` 合并** | 职责不同：`cursors` = "大任务断点"；`tasks` = "事件队列" |
| **超时不算失败** | 游标已存 → 下次续跑；API 返回 `running` 而非 `failed` |

---

## 九、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `metrics` | 无外键 | 任务运行时可打点（如 `maintenance_success`） |
| `tasks` | 无外键 | 两者独立；`tasks` 是事件队列，`cursors` 是断点记录 |
| `subjects` / `knowledge_points` | 无外键 | Seed 导入任务通过 `task_name='seed_*'` 关联 |

---

## 十、索引

```sql
-- cursors 无独立索引：last_status 基数极低，全表扫描即可
```

**索引策略**：
- `task_name` 的 UNIQUE 约束已隐含索引——服务"按任务名查询"
- `last_status` 基数极低（4 个状态），不建独立索引，全表扫描即可
- 无其他查询模式

---


# `tasks` 表设计（待办队列）

## 一、业务用途

事件驱动的待办队列：记录"有事要做"，由定时任务（cron / 启动时）消费。

**核心场景**：
- OCR 完成 → 发现错题 → 写一条 `aggregate_gaps` 任务（待聚合该孩子的诊断结果）
- 每年 9/1 → 写一条 `advance_grade` 任务（推进孩子年级）
- 未来：备份、全量重算等事件触发

**边界**：
- **不是调度器** —— 触发由应用层（启动时 / 定时器 / 外部 cron）负责
- **不是任务执行日志** —— 执行明细见应用日志；本表只记录"待办 / 已办"
- **不存业务数据** —— 业务上下文全在 `detail`（JSON 载荷）
- **不是 `cursors`** —— `cursors` 是"大任务断点位置"；`tasks` 是"事件队列"

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 任务唯一主键 |
| `kind` | `INTEGER` | NOT NULL | 无 | 任务类型枚举：1=aggregate_gaps 2=advance_grade 3=backup 4=cleanup_storage 5=reorder_pages 99=other |
| `priority` | `INTEGER` | NOT NULL | `50` | 优先级（0~100，越大越先跑） |
| `status` | `INTEGER` | NOT NULL | `0` | 状态枚举：0=pending 1=running 2=done 3=failed 4=skipped |
| `retry_count` | `INTEGER` | NOT NULL | `0` | 失败重试次数 |
| `last_error` | `TEXT` | NULL | NULL | 最近一次失败原因（截断保存） |
| `detail` | `TEXT` | NOT NULL | `'{}'` | 任务上下文载荷（JSON）；各 `kind` 自定义结构 |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 任务入队时间 |
| `started_at` | `DATETIME` | NULL | NULL | 开始执行时间 |
| `finished_at` | `DATETIME` | NULL | NULL | 完成 / 失败时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE tasks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         INTEGER NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 50,
    status       INTEGER NOT NULL DEFAULT 0,
    retry_count  INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    detail       TEXT NOT NULL DEFAULT '{}',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at   DATETIME,
    finished_at  DATETIME
);

CREATE INDEX idx_tasks_status_priority ON tasks(status, priority DESC, created_at ASC);
```

---

## 四、枚举值定义（Go 侧）

### `kind`

| 值 | 常量 | 含义 | `detail` 载荷示例 |
|----|------|------|-------------------|
| 1 | `TaskKindAggregateGaps` | 聚合诊断结果 | `{"kid_id": 1}` |
| 2 | `TaskKindAdvanceGrade` | 年级推进 | `{"kid_id": 1, "enrolled_year": 2026, "enrolled_grade": 7, "target_grade": 8}` |
| 3 | `TaskKindBackup` | 备份 | `{}` |
| 4 | `TaskKindCleanupStorage` | 文件清理 | `{"dir": "assets/exams/14"}` |
| 5 | `TaskKindReorderPages` | 删 / 加 / 拖排试卷页（一次请求一个任务，见第七章 7.1 节） | `{"exam_id":14,"task_dir":"assets/tmp/8f3a…","temp_files":[{"path":"assets/tmp/8f3a…/new_0.webp","sha256":"…"}],"final_order":["11","13","new_0"],"page_files":{"11":"assets/exams/14/page_001.webp","12":"assets/exams/14/page_002.webp","13":"assets/exams/14/page_003.webp"},"delete_page_ids":[12]}` |
| 99 | `TaskKindOther` | 其他 | 视情况 |

**未来按需扩展** —— 加任务类型不改表结构。

### `status`

| 值 | 常量 | 含义 |
|----|------|------|
| 0 | `TaskStatusPending` | 待执行 |
| 1 | `TaskStatusRunning` | 执行中 |
| 2 | `TaskStatusDone` | 已完成 |
| 3 | `TaskStatusFailed` | 失败（超过重试上限） |
| 4 | `TaskStatusSkipped` | 已跳过（如条件不满足） |
| 5 | `TaskStatusCancelled` | 已取消（终态，与已完成/已跳过同列清理口径） |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

---

## 五、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 主键 |
| `kind` | 任务类型；应用层按类型分派到不同处理函数 |
| `priority` | 调度排序；越高越先跑 |
| `status` | 状态机；应用层只取 `pending` 处理 |
| `retry_count` | 失败重试计数；达上限转 `failed` |
| `last_error` | 失败原因；运维可见；便于排查 |
| `detail` | 业务上下文载荷（JSON）；Handler 据此定位目标实体与参数 |
| `created_at` | 入队时间；同 `priority` 时按先后顺序 |
| `started_at` | 开始执行时间；审计 |
| `finished_at` | 完成时间；审计；计算耗时 |

---

## 六、入队策略（允许重复 + Handler 幂等）

**Payload-Only 下不再做"入队去重"** —— 允许重复入队，靠 Handler 全量重算保证幂等。

同一孩子多份试卷 OCR 完成 → 可能入队多条 `aggregate_gaps(kid_id=1)`；Worker 逐条消费，每条都全量重算 → 结果相同（幂等）。

**为何不担心重复**：
- `aggregate_gaps` 是**全量重算**（从 `questions` 重数），不是增量累加——重跑不翻倍
- `cleanup_storage` 是 `RemoveAll`——目录不存在时返回 nil，重跑安全
- `advance_grade` 若重复 —— 目标年级公式与幂等门**见 `kids` 表第五章 step 3**（不在本表复述）

> **若某类任务确实需要"同时只一条 pending"**：在 Handler 入口用 `detail` 内字段查一次——按需加，不默认。

---

## 七、应用流程

### 7.1 入队（事件触发）

```
OCR 完成 → 检查 questions 是否有 is_correct=false
  ├─ 有 → 入队 tasks(kind=1, detail={"kid_id": kid_id}, status=0)
  └─ 无 → 不入队
```

### 7.2 消费（定时任务）

```
定时器触发（每 N 分钟 / 每小时）：
1. 取下一个待办：
   SELECT * FROM tasks
   WHERE status = 0
   ORDER BY priority DESC, created_at ASC
   LIMIT 1;
2. 若无 → 休眠
3. 有 → 
   a. UPDATE status = 1（running）, started_at = now
   b. 按 kind 分派执行：
      - kind=1：聚合该 kid 的 gaps → UPDATE gaps 表
      - kind=2：推进该 kid 的年级
      - kind=3：执行备份
   c. 成功 → UPDATE status = 2（done）, finished_at = now
   d. 失败 → UPDATE status = 3（failed）, retry_count += 1, last_error = ?
      - 若 retry_count < 3 → status 重置为 0（重试）
      - 若 retry_count ≥ 3 → 保持 failed
4. 继续下一个（循环，直到无 pending）
```

### 7.3 查询（运维）

```sql
-- 待办数量
SELECT COUNT(*) FROM tasks WHERE status = 0;

-- 失败任务
SELECT * FROM tasks WHERE status = 3 ORDER BY finished_at DESC;
```

**体检页展示**："待办 3 项 / 失败 0 项"

### 7.4 清理（可选）

```sql
-- 清 30 天前的 done / failed / skipped
DELETE FROM tasks
WHERE status IN (2, 3, 4)
  AND finished_at < date('now', '-30 days', 'localtime');
```

---

## 八、设计决策记录

| 决策 | 理由 |
|------|------|
| **表名 `tasks`（非 `jobs`）** | UI 友好（"任务"）；与 `cursors` 语义对仗 |
| **`kind` 用 INT 枚举** | 与全局枚举规范一致；加类型不改 schema |
| **无 `target_id` 列（Payload-Only）** | 业务上下文统一进 `detail`（JSON）；免去多态列的 NULL 语义歧义 |
| **`priority` 支持调度排序** | 未来任务多时按优先级处理 |
| **`status` 用 INT 枚举** | 五态：pending / running / done / failed / skipped |
| **`retry_count` + `last_error`** | 失败可重试（≤ 3 次）；达上限转 failed |
| **`detail` 存 JSON** | 附加参数灵活；不为每种任务加列 |
| **允许重复入队** | Handler 全量重算幂等；不靠入队去重（`detail` 载荷随任务走） |
| **不与 `cursors` 合并** | 职责不同：`tasks` = 事件队列；`cursors` = 大任务断点 |
| **30 天清理已完成** | 控制表大小；失败任务保留 |

---

## 九、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `kids` | Payload-Only，`detail.kid_id`（kind=1/2 时） | 关联孩子 |
| `gaps` | `tasks.kind=1` 时聚合写入 | 任务产物 |
| `metrics` | 无外键 | 任务执行时打点（如 `maintenance_success`） |
| `cursors` | 无外键 | 两者独立；`tasks` 队列 vs `cursors` 断点 |

> **Payload-Only** —— `detail` 是 JSON，**不建物理外键**。Handler 消费时自行查目标实体；查不到则标 `skipped`。

- **优点**：一表容纳所有任务类型
- **缺点**：无参照完整性；删除 `kids` 后 `tasks` 里的孤儿任务需 Handler 兜底

> **清理策略**：删 `kids` 时**不主动清 `tasks`** —— Worker 消费到该 kid 的任务时，Handler 内查 `kids` 不存在 → `UPDATE tasks SET status=4(skipped)`。

---

## 十、索引

```sql
CREATE INDEX idx_tasks_status_priority ON tasks(status, priority DESC, created_at ASC);
```

**索引策略**：
- `(status, priority DESC, created_at ASC)` 组合索引服务"取下一个待办"——最优路径
- 无其他查询模式

---

## 十一、与其他表的区分

| 表 | 职责 | 生命周期 |
|----|------|----------|
| `tasks` | **事件队列** —— "有事要做" | 短（入队 → 消费 → 清理） |
| `cursors` | **断点位置** —— "大任务跑到哪" | 长（持续更新） |
| `metrics` | **观测聚合** —— "当日发生多少次" | 中（90 天） |

**三者协同**：
- 事件 → `tasks` 入队
- 大任务 → `cursors` 记断点
- 执行 → `metrics` 打点

---

## 十二、未来扩展

| 场景 | 是否需要改表 |
|------|--------------|
| 加新任务类型（如 `rebuild_mastery`） | **不需要** —— 加 Go 常量 + 处理函数 |
| 加任务依赖（A 完成后才跑 B） | **需要** —— 加 `depends_on_task_id` 列 |
| 加任务调度时间（"某时刻才可跑"） | **需要** —— 加 `scheduled_at` 列 |
| 加任务分组 / 批次 | **需要** —— 加 `batch_id` 列 |

**当前不做** —— YAGNI。


# `questions` 表设计（题目）

> **状态：预留未接线** —— 本表 schema 已冻结，后端尚无读写方（`aggregate_gaps` 任务保持占位 `skipped`），留待后续立项。

## 一、业务用途

存储试卷 OCR 切出的每一道题：题面、题图、学生答案、标准答案、对错判定、来源标记、审核痕迹。

**核心场景**：
- OCR + AI 处理试卷后，切出 N 道题 → N 条 `questions` 行
- 题面从 OCR 文字提取（Markdown + LaTeX）；题图从试卷页图裁切
- 题目关联知识点（`question_kps`）
- 定时聚合 → `levels` → `gaps`
- 家长/孩子按需审核，追加审核意见（`review_note`）

**边界**：
- **一题一行、一题一图**（OCR 从页图裁切整个题目区域）
- **无 `score` / `full_score`** —— 只判对错（`is_correct`）
- **无 `difficulty`** —— AI 估不准；家庭不需要
- **不存"作答会话"** —— 作答者 = `exams.kid_id`（通过 JOIN 推导）
- **不存裁切坐标** —— 题图已裁好，展示用即可；定位原图靠 `page_id`
- **`review_note` 追加式** —— 保留原始 + 无限追加审核意见

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 题目唯一主键 |
| `exam_id` | `INTEGER` | NOT NULL, FK → `exams.id` | 无 | 所属试卷。`ON DELETE CASCADE` |
| `page_id` | `INTEGER` | FK → `pages.id` | NULL | 题目所在页。`ON DELETE SET NULL` |
| `question_no` | `INTEGER` | NOT NULL | 无 | 卷内题号（从 1 开始） |
| `question_type` | `INTEGER` | NOT NULL | 无 | 题型 INT 枚举（1=choice 2=fill 3=judge ... 99=other） |
| `content` | `TEXT` | NULL | NULL | 题面（Markdown + LaTeX） |
| `image_path` | `TEXT` | NULL | NULL | 题图路径（OCR 从页图裁切；无图题为 NULL） |
| `image_sha256` | `TEXT` | NULL | NULL | 题图完整 SHA256（仅完整性校验） |
| `student_answer` | `TEXT` | NULL | NULL | 学生作答（Markdown + LaTeX；OCR 识别或空） |
| `correct_answer` | `TEXT` | NULL | NULL | 标准答案（Markdown + LaTeX；AI 生成或空） |
| `is_correct` | `INTEGER` | NOT NULL | `0` | 是否全对：0=错 / 1=对 |
| `content_source` | `INTEGER` | NULL | NULL | 题面来源：1=ocr 2=parent |
| `answer_source` | `INTEGER` | NULL | NULL | 答案来源：1=ai 2=parent |
| `is_correct_source` | `INTEGER` | NULL | NULL | 对错来源：1=ai 2=parent 3=kid |
| `review_status` | `INTEGER` | NOT NULL | `0` | 审核状态：0=pending 1=confirmed 2=corrected |
| `reviewed_by` | `INTEGER` | NULL | NULL | 审核人 ID（`users.id` 或 `kids.id`，由 `reviewed_by_type` 区分） |
| `reviewed_by_type` | `INTEGER` | NULL | NULL | 审核人类型：1=parent 2=kid |
| `reviewed_at` | `DATETIME` | NULL | NULL | 最近一次审核时间 |
| `review_note` | `TEXT` | NULL | NULL | 审核痕迹（Markdown 追加式；`<br />` 分隔） |
| `status` | `INTEGER` | NOT NULL | `1` | 状态：1=active 2=deleted |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 创建时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 最后更新时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE questions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    exam_id             INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    page_id             INTEGER REFERENCES pages(id) ON DELETE SET NULL,
    question_no         INTEGER NOT NULL,
    question_type       INTEGER NOT NULL,

    content             TEXT,
    image_path          TEXT,
    image_sha256        TEXT,
    student_answer      TEXT,
    correct_answer      TEXT,
    is_correct          INTEGER NOT NULL DEFAULT 0,

    content_source      INTEGER,
    answer_source       INTEGER,
    is_correct_source   INTEGER,

    review_status       INTEGER NOT NULL DEFAULT 0,
    reviewed_by         INTEGER,
    reviewed_by_type    INTEGER,
    reviewed_at         DATETIME,
    review_note         TEXT,

    status              INTEGER NOT NULL DEFAULT 1,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exam_id, question_no)
);

CREATE INDEX idx_questions_exam ON questions(exam_id);
```

---

## 四、枚举值定义（Go 侧）

> **预留契约，未实现（表已建、无读写方）**：以下枚举是题目链（OCR → 审核 → 聚合）的设计口径，仓储与算法尚未落地；描述保留设计意图。

### `question_type`

| 值 | 常量 | 显示 |
|----|------|------|
| 1 | `QuestionTypeChoice` | 选择题 |
| 2 | `QuestionTypeFill` | 填空题 |
| 3 | `QuestionTypeJudge` | 判断题 |
| 4 | `QuestionTypeSolve` | 解答题 |
| 5 | `QuestionTypeCompute` | 计算题 |
| 6 | `QuestionTypeProve` | 证明题 |
| 7 | `QuestionTypeExperiment` | 实验题 |
| 8 | `QuestionTypeDraw` | 作图题 |
| 9 | `QuestionTypeShortAnswer` | 简答题 |
| 10 | `QuestionTypeReading` | 阅读题 |
| 11 | `QuestionTypeCloze` | 完形填空 |
| 12 | `QuestionTypeTranslate` | 翻译题 |
| 13 | `QuestionTypeDictation` | 默写题 |
| 14 | `QuestionTypeWriting` | 写作题 |
| 15 | `QuestionTypeComprehensive` | 综合题 |
| 99 | `QuestionTypeOther` | 其他 |

### `content_source`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `ContentSourceOCR` | OCR 识别 |
| 2 | `ContentSourceParent` | 家长手动修正 |

### `answer_source`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `AnswerSourceAI` | AI 生成 |
| 2 | `AnswerSourceParent` | 家长手动修正 |

### `is_correct_source`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `IsCorrectSourceAI` | AI 判对错 |
| 2 | `IsCorrectSourceParent` | 家长改对错 |
| 3 | `IsCorrectSourceKid` | 孩子标对错 |

### `review_status`

| 值 | 常量 | 含义 |
|----|------|------|
| 0 | `ReviewStatusPending` | 未审核（AI 产出） |
| 1 | `ReviewStatusConfirmed` | 已确认 |
| 2 | `ReviewStatusCorrected` | 已纠正 |

### `reviewed_by_type`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `ReviewerTypeParent` | 家长 |
| 2 | `ReviewerTypeKid` | 孩子 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

---

## 五、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 被 `question_kps.question_id` 引用 |
| `exam_id` | 关联试卷；作答者 = `exams.kid_id` |
| `page_id` | 关联原图页；UI 可"点击定位原图" |
| `question_no` | 卷内题号；UI 排序 |
| `question_type` | UI 显示题型；未来按题型筛选 |
| `content` | UI 显示题面（Markdown 渲染） |
| `image_path` | UI 显示题图（无图题为空） |
| `image_sha256` | 仅完整性校验 |
| `student_answer` | UI 显示"孩子写了什么" |
| `correct_answer` | UI 显示"正确答案" |
| `is_correct` | 判对错；聚合掌握度的依据 |
| `content_source` | UI 显示"OCR 识别" / "家长修正" |
| `answer_source` | UI 显示"AI 生成" / "家长修正" |
| `is_correct_source` | UI 显示"AI 判" / "家长改" / "孩子标" |
| `review_status` | UI 显示"待审" / "已确认" / "已纠正" |
| `reviewed_by` | 用于显示审核人名字（JOIN `users` / `kids`） |
| `reviewed_by_type` | 区分审核人来自 `users` 还是 `kids` |
| `reviewed_at` | 审计 |
| `review_note` | UI 显示审核痕迹（Markdown 追加式） |
| `status` | `1` 正常；`2` 软删 |
| `created_at` / `updated_at` | 审计 |

---

## 六、应用流程

### 6.1 OCR + AI 产出

```
1. 试卷 OCR 完成 → 切出 N 道题
2. 对每道题：
   a. 从页图裁切题目区域 → 存题图 → image_path / image_sha256
   b. INSERT questions(
        exam_id, page_id, question_no, question_type,
        content, image_path, image_sha256, student_answer,
        correct_answer, is_correct,
        content_source=1(ocr), answer_source=1(ai), is_correct_source=1(ai),
        review_status=0(pending),
        status=1
      )
3. AI 打知识点 → INSERT question_kps
4. 定时任务触发 → 聚合 levels / gaps
```

### 6.2 家长/孩子审核

```
家长/孩子打开题目详情：
1. UI 显示：题面 / 题图 / 学生答案 / AI 答案 / AI 判对错
2. UI 显示：审核记录（review_note）

家长追加审核：
1. 点"追加审核"
2. 输入审核意见（Markdown，可为空）
3. 提交：
   - 若意见非空：
     a. review_note = appendReviewNote(review_note, 意见, display_name)
     b. review_status = 2（corrected）
   - 若意见为空：
     a. review_status = 1（confirmed）
   c. reviewed_by = 家长 ID
   d. reviewed_by_type = 1（parent）
   e. reviewed_at = now
4. 触发重新聚合（可选，按 tasks）

孩子标对错：
1. 点"我做对了" / "我做错了"
2. 提交：
   - is_correct = 新值
   - is_correct_source = 3（kid）
   - review_note = appendReviewNote(review_note, "孩子标：我做对了", display_name)
   - reviewed_by = 孩子 ID
   - reviewed_by_type = 2（kid）
   - reviewed_at = now
3. 触发重新聚合（按 tasks）
```

**学科一致性提示**：审核页面加载时，比对本题所有 `question_kps` 关联的 KP 学科（`knowledge_points.subject_id`）与所属试卷 `exam.subject_id`；若不一致，页面顶部提示"该题知识点学科与试卷学科不符，请审核"——人工确认是否修正 KP 归属（分工见 `exams` 表第四章"枚举值定义"中"学科归属与筛选（分工）"段）。

### 6.3 `review_note` 追加算法

```go
func appendReviewNote(existing, newNote, displayName string) string {
    if newNote == "" {
        return existing
    }
    entry := newNote + "...（" + displayName + "）审核"
    if existing == "" {
        return entry
    }
    return existing + "<br />" + entry
}
```

**示例**：

第一次（爸爸审）：
```
$b$ 应该等于 $-2$。...（爸爸）审核
```

第二次（妈妈审）：
```
$b$ 应该等于 $-2$。...（爸爸）审核<br />本题选 B，不是 A。...（妈妈）审核
```

第三次（孩子标对错）：
```
$b$ 应该等于 $-2$。...（爸爸）审核<br />本题选 B，不是 A。...（妈妈）审核<br />孩子标：我做对了。...（小明）审核
```

### 6.4 数据查询

**某孩子的所有错题**（JOIN `exams`）：
```sql
SELECT q.*
FROM questions q
JOIN exams e ON e.id = q.exam_id
WHERE e.kid_id = ? AND e.status = 1 AND q.is_correct = 0 AND q.status = 1;
```

**某孩子的所有待审题**：
```sql
SELECT q.*
FROM questions q
JOIN exams e ON e.id = q.exam_id
WHERE e.kid_id = ? AND e.status = 1 AND q.review_status = 0 AND q.status = 1;
```

**某知识点的题**（JOIN `question_kps`）：
```sql
SELECT q.*
FROM questions q
JOIN question_kps qkp ON qkp.question_id = q.id
JOIN exams e ON e.id = q.exam_id
WHERE qkp.knowledge_point_id = ? AND e.status = 1 AND q.status = 1;
```

### 6.5 OCR 重切（同 exam 重新跑 OCR）

**问题**：重切时新题集与旧题集 `question_no` 必撞 `UNIQUE(exam_id, question_no)`。

**替换协议（单事务）**：
1. BEGIN
2. `DELETE FROM questions WHERE exam_id = ?`——级联清 `question_kps`；`gaps.latest_question_id` 置 NULL
3. 从页图重新裁切 → 落盘新题图（覆盖 `assets/exams/{id}/questions/q_*.webp`）
4. `INSERT questions(...)` × N（新题集）
5. `INSERT question_kps(...)` × M（AI 重新打标）
6. 入队 `tasks(kind=1, detail={"kid_id":X})`（重新聚合）
7. COMMIT

> 注：重切前应提示"将覆盖旧题集与审核痕迹"；如需保留旧版，用软删（`status=2`）+ 新题 `status=1`，但需改 `UNIQUE` 为 `UNIQUE(exam_id, question_no, status)`——**当前不做**（YAGNI）。

---

## 七、设计决策记录

| 决策 | 理由 |
|------|------|
| **无 `kid_id`** | 作答者 = `exams.kid_id`（JOIN 推导）；避免冗余 |
| **无 `score` / `full_score`** | 只判对错（`is_correct`）；避免家长填分的负担 |
| **无 `difficulty`** | AI 估不准；家庭不需要 |
| **一题一图（`image_path` + `image_sha256`）** | OCR 从页图裁切整个题目区域；不存 JSON 数组 |
| **不存裁切坐标** | 题图已裁好；定位原图靠 `page_id`；重切让 OCR 重跑 |
| **`UNIQUE(exam_id, question_no)`** | 一份试卷内题号唯一 |
| **`content` / `student_answer` / `correct_answer` 存 Markdown + LaTeX** | 数学公式统一格式；前端 `md-editor-v3` 渲染 |
| **`is_correct` 用 INT** | 0 / 1 |
| **`*_source` 三字段** | 每字段独立标记来源；追溯 OCR / AI / 人工 |
| **`review_status` 三态** | pending / confirmed / corrected |
| **`reviewed_by` + `reviewed_by_type`** | 记 ID + 类型 → 可显示"爸爸" / "小明" |
| **`review_note` 追加式** | 保留原始 + 追加审核痕迹（谁改了什么、改了几次）；非沟通工具 |
| **审核不强制** | 家庭工具；家长按需审核 |
| **`status` 软删** | `1` 正常；`2` 软删（家长误删可恢复） |
| **`reviewed_by` 无 FK** | 多态引用 `users.id` / `kids.id`，不建物理外键；家长账号物理删后该列悬空——UI JOIN 时判空显示"（已删除）"（家庭场景低频） |

---

## 八、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `exams` | `questions.exam_id → exams.id` | 所属试卷（`ON DELETE CASCADE`） |
| `pages` | `questions.page_id → pages.id` | 题目所在页（`ON DELETE SET NULL`） |
| `question_kps` | `question_kps.question_id → questions.id` | 题 ↔ 知识点 |
| `kids` | 通过 `exams.kid_id` 间接关联 | 作答者 |
| `users` | 通过 `questions.reviewed_by` + `reviewed_by_type=1` 关联 | 审核人（家长） |
| `kids` | 通过 `questions.reviewed_by` + `reviewed_by_type=2` 关联 | 审核人（孩子） |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 删 `exams` | `CASCADE` 删其所有 `questions` |
| 删 `pages` | `SET NULL`（题保留，`page_id` 置空） |
| 删 `questions` | `CASCADE` 删其 `question_kps` 关联 |

---

## 九、索引

```sql
CREATE INDEX idx_questions_exam ON questions(exam_id);
```

**索引策略**：
- `exam_id` 索引服务"查某试卷的所有题"
- `status` / `review_status` 基数低，不建独立索引
- `UNIQUE(exam_id, question_no)` 隐含组合索引

**未来可选**（若查询慢再补）：
- `(exam_id, is_correct)` —— 服务"查某卷的错题"
- `(exam_id, review_status)` —— 服务"查某卷的待审题"

---

## 十、文件系统布局

```
/data/paperly/
└── assets/
    └── exams/
        ├── 14/
        │   ├── page_001.webp        # 试卷页原图
        │   ├── page_002.webp
        │   └── questions/            # 本卷切出的题图
        │       ├── q_001.webp
        │       └── q_002.webp
        └── 15/
            └── ...
```

**规则**：
- 题图**归属其试卷目录**（`assets/exams/{exam_id}/questions/`）
- 文件名：`q_{question_no:03d}.{ext}`（与 `questions.question_no` 对应）
- **不跨卷去重**；同图重复落盘可接受（单机家庭场景）
- 删卷 → `RemoveAll("assets/exams/{exam_id}/")` 一并清掉题图（见 `exams` 表 6.6 节）


---


# `question_kps` 表设计（题目与知识点关联）

> **状态：预留未接线** —— 本表 schema 已冻结，后端尚无读写方（`aggregate_gaps` 任务保持占位 `skipped`），留待后续立项。

## 一、业务用途

管理题目与知识点之间的多对多关联：一道题可考察 1~3 个知识点，一个知识点可覆盖多道题。

**核心场景**：
- OCR + AI 切题后，自动提取题目涉及的知识点 → 建关联
- 家长/孩子在题目详情页手动新增、删除、调整关联
- 聚合任务反查"某孩子在指定知识点下的全部作答题目"

**边界**：
- **不做连续权重**（不引入 `0.35` 这类微积分式权重）——仅用 `is_primary` 区分主要/次要考点
- **依赖实体存在**：题或知识点删除 → 关联随外键级联清除
- **关联唯一**：同一题 ↔ 同一知识点只一条

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 关联记录唯一主键 |
| `question_id` | `INTEGER` | NOT NULL, FK → `questions.id` | 无 | 关联题目。`ON DELETE CASCADE` |
| `knowledge_point_id` | `INTEGER` | NOT NULL, FK → `knowledge_points.id` | 无 | 关联知识点。`ON DELETE CASCADE` |
| `is_primary` | `INTEGER` | NOT NULL | `1` | 是否核心考点：`1`=主要考点，`0`=次要/结合考点 |
| `source` | `INTEGER` | NOT NULL | `1` | 关联来源：`1`=AI，`2`=家长，`3`=孩子 |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 关联建立时间 |

---

## 三、建表 SQL

```sql
CREATE TABLE question_kps (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    question_id         INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    knowledge_point_id  INTEGER NOT NULL REFERENCES knowledge_points(id) ON DELETE CASCADE,
    is_primary          INTEGER NOT NULL DEFAULT 1,
    source              INTEGER NOT NULL DEFAULT 1,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(question_id, knowledge_point_id)
);

CREATE INDEX idx_qkp_question ON question_kps(question_id);
CREATE INDEX idx_qkp_kp ON question_kps(knowledge_point_id);
```

---

## 四、枚举值定义（Go 侧）

> **预留契约，未实现（表已建、无读写方）**：以下枚举是题目–知识点关联链的设计口径，随题目链落地；描述保留设计意图。

### `source`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `QKPSourceAI` | AI 自动提取或标注 |
| 2 | `QKPSourceParent` | 家长手动标注或调整 |
| 3 | `QKPSourceKid` | 孩子自行标注 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

---

## 五、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 主键 |
| `question_id` | 关联题目；删题时级联清除 |
| `knowledge_point_id` | 关联知识点；聚合任务按此反查某 KP 下的全部作答 |
| `is_primary` | **仅 UI 主次标注**；**不参与掌握度计算**（`levels`/`gaps` 聚合 SQL 全计主次，无 `WHERE is_primary=1`） |
| `source` | UI 显示关联来源；审计"是谁标的" |
| `created_at` | 审计 |

---

## 六、应用流程

### 6.1 AI 自动建立关联

```
1. OCR 切题 → INSERT questions
2. AI 识别该题涉及的知识点（1~3 个）
3. 对每个 KP：
   INSERT question_kps(question_id, knowledge_point_id, is_primary=?, source=1)
4. 触发聚合任务（写入 tasks）
```

### 6.2 家长/孩子手动调整

```
新增：
POST /api/questions/:id/kps
{knowledge_point_id: 12, is_primary: 1}
→ INSERT question_kps(..., source=2 或 3)

删除：
DELETE /api/questions/:id/kps/:kp_id
→ DELETE FROM question_kps WHERE question_id=? AND knowledge_point_id=?

改主次：
PUT /api/questions/:id/kps/:kp_id
{is_primary: 0}
→ UPDATE question_kps SET is_primary=? WHERE ...
```

**冲突**：`UNIQUE(question_id, knowledge_point_id)` 保证重复插入报错，应用层提示"该知识点已关联"。

### 6.3 聚合任务反查

```sql
-- 某孩子在指定 KP 下的全部题目
SELECT q.*
FROM questions q
JOIN question_kps qkp ON qkp.question_id = q.id
JOIN exams e ON e.id = q.exam_id
WHERE e.kid_id = ? AND qkp.knowledge_point_id = ? AND e.status = 1 AND q.status = 1;
```

---

## 七、设计决策记录

| 决策 | 理由 |
|------|------|
| **独立成表（多对多）** | 一题多 KP、一 KP 多题，必须用关联表 |
| **表名 `question_kps`（非 `question_knowledge_points`）** | 与短表名风格一致（`kids`/`exams`）；避免超长表名 |
| **`UNIQUE(question_id, knowledge_point_id)`** | 同题同 KP 只一条；DB 层兜底 |
| **`is_primary` 而非 `weight`** | 家庭场景不做连续权重；主/次二分足够 |
| **`source` 三态** | 追溯"AI 标的 / 家长改的 / 孩子标的" |
| **双 `ON DELETE CASCADE`** | 题或 KP 任一删除 → 关联自动清；不留孤儿 |
| **无 `updated_at`** | 关联记录修改频率低；`source`/`is_primary` 变更可接受不记时间（如需再补） |

---

## 八、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `questions` | `question_kps.question_id → questions.id` | 关联题目（`ON DELETE CASCADE`） |
| `knowledge_points` | `question_kps.knowledge_point_id → knowledge_points.id` | 关联知识点（`ON DELETE CASCADE`） |
| `levels`（下一张表） | 无外键；聚合任务按 KP 反查 | 掌握度聚合的输入 |
| `gaps`（下一张表） | 无外键；聚合任务按 KP 反查 | 薄弱点诊断的输入 |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 删 `questions` 行 | `CASCADE` 删其所有 `question_kps` |
| 删 `knowledge_points` 行 | `CASCADE` 删其所有 `question_kps` |

---

## 九、索引

```sql
CREATE INDEX idx_qkp_question ON question_kps(question_id);
CREATE INDEX idx_qkp_kp ON question_kps(knowledge_point_id);
```

**索引策略**：
- `question_id` 索引服务"查某题的所有知识点"
- `knowledge_point_id` 索引服务"查某 KP 的所有题"（聚合任务核心路径）
- `UNIQUE(question_id, knowledge_point_id)` 隐含组合索引

---


# `levels` 表设计（孩子知识点掌握水平）

> **状态：预留未接线** —— 本表 schema 已冻结，后端尚无读写方（`aggregate_gaps` 任务保持占位 `skipped`），留待后续立项。

## 一、业务用途

按"孩子 + 知识点"维度，持久化统计指标与掌握水平评级。

**核心场景**：
- 展示孩子的知识树掌握状态（未练 / 薄弱 / 良好 / 已掌握）
- 为雷达图、知识点总览提供数据
- 为 `gaps` 薄弱点诊断提供定量输入
- 由后台待办任务异步批量刷新（`tasks.kind=aggregate_gaps`），避开录题事务做大范围统计

**边界**：
- **不逐题强更新**：题目新增/审核完 → 仅写聚合任务，Worker 批量刷新
- **存储态精简**：废弃递归衰减树，只保留做对数、做错数、最近 3 题错数、水平
- **无 `subject_id`**：学科从 `knowledge_point_id` 推导（`knowledge_points.subject_id`），不冗余
- **无 `accuracy_rate`**：派生值，查询时由 `correct_count / (correct_count + wrong_count)` 算
- **全量重算**：聚合任务按 KP 重跑，不是增量累加（重跑安全）

---

## 二、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 唯一主键 |
| `kid_id` | `INTEGER` | NOT NULL, FK → `kids.id` | 无 | 所属孩子。`ON DELETE CASCADE` |
| `knowledge_point_id` | `INTEGER` | NOT NULL, FK → `knowledge_points.id` | 无 | 知识点。`ON DELETE CASCADE` |
| `correct_count` | `INTEGER` | NOT NULL | `0` | 累计做对题数 |
| `wrong_count` | `INTEGER` | NOT NULL | `0` | 累计做错题数 |
| `recent_wrong_count` | `INTEGER` | NOT NULL | `0` | 最近 3 题中的错题数，`0~3` |
| `level` | `INTEGER` | NOT NULL | `0` | 掌握水平：`0`=未训练 `1`=薄弱 `2`=良好 `3`=已掌握 |
| `first_practiced_at` | `DATETIME` | NULL | NULL | 首次做该 KP 的时间 |
| `last_practiced_at` | `DATETIME` | NULL | NULL | 最近一次做该 KP 的时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 统计更新时间 |

> **`total_count` 不存**：`= correct_count + wrong_count`，纯派生。
> **`accuracy_rate` 不存**：`= correct_count / (correct_count + wrong_count) * 100`，纯派生。
> **`subject_id` 不存**：从 `knowledge_point_id` JOIN `knowledge_points.subject_id` 推导。

---

## 三、建表 SQL

```sql
CREATE TABLE levels (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    kid_id              INTEGER NOT NULL REFERENCES kids(id) ON DELETE CASCADE,
    knowledge_point_id  INTEGER NOT NULL REFERENCES knowledge_points(id) ON DELETE CASCADE,
    correct_count       INTEGER NOT NULL DEFAULT 0,
    wrong_count         INTEGER NOT NULL DEFAULT 0,
    recent_wrong_count  INTEGER NOT NULL DEFAULT 0,
    level               INTEGER NOT NULL DEFAULT 0,
    first_practiced_at  DATETIME,
    last_practiced_at   DATETIME,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(kid_id, knowledge_point_id)
);

CREATE INDEX idx_levels_kid_level ON levels(kid_id, level);
```

---

## 四、枚举值定义（Go 侧）

> **预留契约，未实现（表已建、无读写方）**：以下枚举是掌握度聚合链的设计口径，随聚合任务落地；描述保留设计意图。

### `level`

| 值 | 常量 | 含义 |
|----|------|------|
| 0 | `LevelUntrained` | 未训练 / 样本不足 |
| 1 | `LevelWeak` | 薄弱 |
| 2 | `LevelGood` | 良好 |
| 3 | `LevelMastered` | 已掌握 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

---

## 五、水平判定规则

```go
const (
    LevelUntrained = 0 // 样本不足（< 3 题）
    LevelWeak      = 1 // 薄弱
    LevelGood      = 2 // 良好
    LevelMastered  = 3 // 已掌握
)

// CalculateLevel 纯函数：输入做对数、做错数、最近 3 题错数，输出水平
func CalculateLevel(correct, wrong, recentWrong int) int {
    total := correct + wrong

    // 样本不足
    if total < 3 {
        return LevelUntrained
    }

    rate := float64(correct) / float64(total) * 100.0

    // 正确率过低，或最近 3 题中至少错 2 题
    if rate < 60.0 || recentWrong >= 2 {
        return LevelWeak
    }

    // 样本量充足、正确率较高且最近 3 题全对
    if total >= 5 && rate >= 85.0 && recentWrong == 0 {
        return LevelMastered
    }

    return LevelGood
}
```

| 条件 | 判定结果 |
|------|----------|
| `total < 3` | `LevelUntrained`，样本不足 |
| `total >= 3` 且正确率 `< 60%` | `LevelWeak`，薄弱 |
| `total >= 3` 且 `recent_wrong_count >= 2` | `LevelWeak`，薄弱 |
| `total >= 5`、正确率 `>= 85%` 且最近 3 题全对 | `LevelMastered`，已掌握 |
| 其他情况 | `LevelGood`，良好 |

---

## 六、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 主键 |
| `kid_id` | 关联孩子；级联删除 |
| `knowledge_point_id` | 关联知识点；JOIN 出学科名、KP 名 |
| `correct_count` | UI 显示"对 N 题"；水平判定输入 |
| `wrong_count` | UI 显示"错 N 题"；水平判定输入 |
| `recent_wrong_count` | UI 显示"最近 3 题错 N 题"；水平判定输入 |
| `level` | UI 显示"未练/薄弱/良好/已掌握"；gaps 诊断输入 |
| `first_practiced_at` | UI 显示"首次练于 X"；冷启动判断 |
| `last_practiced_at` | UI 显示"最近练于 X"；"久未练"提醒 |
| `updated_at` | 审计 |

---

## 七、应用流程

### 7.1 聚合任务写入（Worker 消费 `aggregate_gaps`）

```
1. 按 kid_id 反查（**必带 `e.status = 1`**）：
   kids → exams → questions → question_kps → knowledge_points
2. 对每个 (kid_id, knowledge_point_id) 聚合：
   - correct_count      = COUNT(questions WHERE is_correct=1)
   - wrong_count        = COUNT(questions WHERE is_correct=0)
   - recent_wrong_count = 最近 3 题中 is_correct=0 的数量
   - first_practiced_at = MIN(exams.exam_date / questions.created_at)
   - last_practiced_at  = MAX(exams.exam_date / questions.created_at)
3. level = CalculateLevel(correct_count, wrong_count, recent_wrong_count)
4. UPSERT levels：
   INSERT INTO levels (kid_id, knowledge_point_id, correct_count, wrong_count,
                       recent_wrong_count, level, first_practiced_at, last_practiced_at)
   VALUES (?, ?, ?, ?, ?, ?, ?, ?)
   ON CONFLICT(kid_id, knowledge_point_id)
   DO UPDATE SET
       correct_count      = excluded.correct_count,
       wrong_count        = excluded.wrong_count,
       recent_wrong_count = excluded.recent_wrong_count,
       level              = excluded.level,
       first_practiced_at = COALESCE(levels.first_practiced_at, excluded.first_practiced_at),
       last_practiced_at  = excluded.last_practiced_at,
       updated_at         = CURRENT_TIMESTAMP;
   -- first_practiced_at 用 COALESCE 保留首次值，不被覆盖
```

**关键**：聚合是**全量重算**（从 `questions` 重数），不是 `correct_count += 1` 增量——重跑任务不会翻倍。

### 7.2 反查某孩子某学科的知识树

```sql
SELECT l.*, kp.name AS kp_name, s.name AS subject_name
FROM levels l
JOIN knowledge_points kp ON kp.id = l.knowledge_point_id
JOIN subjects s ON s.id = kp.subject_id
WHERE l.kid_id = ? AND kp.subject_id = ?
ORDER BY kp.sort_order ASC;
```

> 学科从 `knowledge_points.subject_id` JOIN 出来，不存 `levels.subject_id`。

### 7.3 反查某孩子的薄弱项

```sql
SELECT l.*, kp.name AS kp_name
FROM levels l
JOIN knowledge_points kp ON kp.id = l.knowledge_point_id
WHERE l.kid_id = ? AND l.level = 1  -- LevelWeak
ORDER BY (CAST(l.correct_count AS REAL) / NULLIF(l.correct_count + l.wrong_count, 0)) ASC;
```

### 7.4 查询时算正确率

```sql
SELECT
    correct_count,
    wrong_count,
    CAST(correct_count AS REAL) / NULLIF(correct_count + wrong_count, 0) * 100 AS accuracy_rate
FROM levels
WHERE kid_id = ? AND knowledge_point_id = ?;
```

---

## 八、设计决策记录

| 决策 | 理由 |
|------|------|
| **表名 `levels`（非 `knowledge_point_levels`）** | 短名风格一致；"level"语义直白 |
| **无 `subject_id`** | 派生自 `knowledge_points.subject_id`；一个事实一个权威源 |
| **无 `total_count`** | `= correct_count + wrong_count`，纯派生 |
| **无 `accuracy_rate`** | 派生值；查询时算，避免不一致 |
| **用 `correct_count` + `wrong_count`（非 `total` + `correct`）** | 直白表达"对/错"；`total` 查询时加 |
| **`recent_wrong_count` 存"最近 3 题错数"** | 水平判定需近期信号；不存则每次查询要排序 |
| **`level` 存 INT 枚举** | 与全局枚举规范一致 |
| **`first_practiced_at` 新增** | 冷启动判断 / "何时开始练这个 KP" |
| **`last_practiced_at` 可空** | 未练时为 NULL |
| **双 `CASCADE`** | 孩 / KP 任一删除 → 水平记录自动清 |
| **不做递归衰减树** | 家庭场景；简化指标，`CalculateLevel` 纯函数判定 |
| **不在录题事务内更新** | 由 `aggregate_gaps` 任务异步批量刷新，避开热点锁 |
| **全量重算 UPSERT** | 重跑安全；避免增量累加翻倍 |

---

## 九、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `kids` | `levels.kid_id → kids.id` | 所属孩子（`ON DELETE CASCADE`） |
| `knowledge_points` | `levels.knowledge_point_id → knowledge_points.id` | 知识点（`ON DELETE CASCADE`） |
| `subjects` | **无外键**；通过 `knowledge_points.subject_id` 间接关联 | 学科（查询时 JOIN） |
| `question_kps` | 无外键；聚合任务按 KP 反查 | 掌握度聚合的输入 |
| `gaps` | 无外键；`level=Weak` 驱动 gaps 新增/更新 | 薄弱点诊断输入 |
| `tasks` | 无外键；`kind=aggregate_gaps` 触发刷新 | 异步载体 |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 删 `kids` 行 | `CASCADE` 删其所有 `levels` |
| 删 `knowledge_points` 行 | `CASCADE` 删其所有 `levels` |
| 删 `subjects` 行 | `CASCADE` 删其下所有 `knowledge_points` → 间接 `CASCADE` 删相关 `levels` |

---

## 十、索引

```sql
CREATE INDEX idx_levels_kid_level ON levels(kid_id, level);
```

**索引策略**：
- `(kid_id, level)` 组合索引服务"查某孩的薄弱项"
- `UNIQUE(kid_id, knowledge_point_id)` 已覆盖"查某孩某 KP 的水平"（UPSERT 与详情页），不另建 `idx_levels_kid_kp`
- **无 `subject_id` 索引**：学科经 JOIN 取得；"按学科筛"走 `knowledge_points.subject_id` 索引 + JOIN

> **可选**：若"按学科查知识树"是高频查询且慢，`knowledge_points.subject_id` 已有 `idx_kp_subject`，JOIN 即可。

---


# `gaps` 表设计（薄弱点与待攻克清单）

> **状态：预留未接线** —— 本表 schema 已冻结，后端尚无读写方（`aggregate_gaps` 任务保持占位 `skipped`），留待后续立项。

## 一、业务用途

存储系统基于 `levels` 诊断出的薄弱项与待攻克任务，回答：

> **现在到底哪里不会？下一步该重点复习什么？**

**核心场景**：
- `aggregate_gaps` 任务执行时，从 `levels` 筛 `level=Weak` → 新增/更新 `gaps`
- 家长端"薄弱点提醒"、孩子端"攻克清单"
- 后续复习任务推荐、错题重做跟踪
- 重做巩固 / 后续考试答对 → 标记 `resolved`

---

## 二、与 `levels` 的关系（定位澄清）

**`gaps` 不是第二张聚合表，是 `levels` 的"筛选 + 状态机 + 诊断层"。**

| 维度 | `levels` | `gaps` |
|------|----------|--------|
| 回答 | "这个 KP 练得怎样？" | "现在该补哪个？" |
| 性质 | 统计表（全量 KP） | 待办清单（仅薄弱 KP） |
| 范围 | 所有练过的 KP | 仅 `level=Weak` |
| 生命周期 | 长期存在 | active → resolved → 可复发 |
| 数据来源 | 从 `questions` 聚合 | **从 `levels` 筛选 + 附加** |
| 状态 | 无（`level` 是快照） | active/resolved/ignored |
| 人工干预 | 无 | 可忽略 |
| 历史 | 无（只反映当下） | 保留产生/解决/复发 |

**字段来源**：

| `gaps` 字段 | 来源 |
|-------------|------|
| `error_count` | **直接取 `levels.wrong_count`**（不重新数题） |
| `severity` | `CalculateSeverity(levels.correct_count, levels.wrong_count, levels.recent_wrong_count)` |
| `reason_hint` | 由 `levels` 指标生成话术 |
| `latest_question_id` | **回查 `questions`**（`levels` 无此信息） |
| `status` | 状态机，由任务/人工驱动 |

**边界**：
- 同一孩子同一 KP **最多一条 active**（部分唯一索引兜底）
- 与具体题目解耦（主体是"薄弱知识点"）
- 保留历史：攻克后置 `resolved`，不删
- 无 `subject_id`（从 `knowledge_points.subject_id` 推导）

---

## 三、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 唯一主键 |
| `kid_id` | `INTEGER` | NOT NULL, FK → `kids.id` | 无 | 所属孩子。`ON DELETE CASCADE` |
| `knowledge_point_id` | `INTEGER` | NOT NULL, FK → `knowledge_points.id` | 无 | 薄弱知识点。`ON DELETE CASCADE` |
| `severity` | `INTEGER` | NOT NULL | `1` | 严重级别：`1`=低 `2`=中 `3`=高 |
| `error_count` | `INTEGER` | NOT NULL | `1` | 历史错题总数（取 `levels.wrong_count`） |
| `latest_question_id` | `INTEGER` | NULL, FK → `questions.id` | NULL | 最近一次错题。`ON DELETE SET NULL` |
| `reason_hint` | `TEXT` | NULL | NULL | 诊断摘要，如"近 3 题连错 2 题，需排查运算法则" |
| `status` | `INTEGER` | NOT NULL | `1` | `1`=待攻克 `2`=已攻克 `3`=已忽略 |
| `resolved_at` | `DATETIME` | NULL | NULL | 达标时间 |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 首次诊断时间 |
| `updated_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 状态/统计更新时间 |

---

## 四、建表 SQL

```sql
CREATE TABLE gaps (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    kid_id              INTEGER NOT NULL REFERENCES kids(id) ON DELETE CASCADE,
    knowledge_point_id  INTEGER NOT NULL REFERENCES knowledge_points(id) ON DELETE CASCADE,
    severity            INTEGER NOT NULL DEFAULT 1,
    error_count         INTEGER NOT NULL DEFAULT 1,
    latest_question_id  INTEGER REFERENCES questions(id) ON DELETE SET NULL,
    reason_hint         TEXT,
    status              INTEGER NOT NULL DEFAULT 1,
    resolved_at         DATETIME,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_gaps_kid_status ON gaps(kid_id, status);
CREATE INDEX idx_gaps_kid_kp ON gaps(kid_id, knowledge_point_id);

-- 部分唯一索引：同一孩子同一 KP 最多一条 active
CREATE UNIQUE INDEX uq_gaps_active_kid_kp
    ON gaps(kid_id, knowledge_point_id)
    WHERE status = 1;
```

---

## 五、部分唯一索引说明

```sql
CREATE UNIQUE INDEX uq_gaps_active_kid_kp
    ON gaps(kid_id, knowledge_point_id)
    WHERE status = 1;
```

SQLite 部分唯一索引，同时满足：

- 同一孩子同一 KP **最多一条 active**
- 攻克后**保留历史**（`status=2/3` 不受约束）
- 该 KP **再次变薄弱**时可**新增 active**（旧 row 已非 active）
- 保留完整**产生 → 解决 → 复发**历史

> `modernc.org/sqlite` 支持部分索引；迁 PostgreSQL 亦支持。

> **`ignored(3)` 语义**：长期静默——聚合阶段 2 见到同 KP 已有 ignored 记录时不再新建 active（家长主动"取消忽略"才恢复监控）。

---

## 六、枚举值定义（Go 侧）

> **预留契约，未实现（表已建、无读写方）**：以下枚举是薄弱点诊断链的设计口径，随聚合任务落地；描述保留设计意图。

### `severity`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `GapSeverityLow` | 轻度，刚跌破及格线 |
| 2 | `GapSeverityMedium` | 中度，正确率 < 50% |
| 3 | `GapSeverityHigh` | 重度，多次连错或核心必考点 |

### `status`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `GapStatusActive` | 待攻克 |
| 2 | `GapStatusResolved` | 已攻克 |
| 3 | `GapStatusIgnored` | 已忽略 |

**注意**：DB 无 CHECK 约束，枚举校验在 service 层。

---

## 七、算法

### 7.1 `CalculateSeverity`（纯函数，与 `CalculateLevel` 同源输入）

```go
const (
    GapSeverityLow    = 1
    GapSeverityMedium = 2
    GapSeverityHigh   = 3
)

// 输入取自 levels，输出严重级别
func CalculateSeverity(correct, wrong, recentWrong int) int {
    total := correct + wrong
    if total == 0 {
        return GapSeverityLow
    }
    rate := float64(correct) / float64(total) * 100.0

    // 重度：正确率 < 50%，或最近 3 题连错 ≥ 2
    if rate < 50.0 || recentWrong >= 2 {
        return GapSeverityHigh
    }
    // 中度：正确率 < 60%
    if rate < 60.0 {
        return GapSeverityMedium
    }
    // 轻度
    return GapSeverityLow
}
```

### 7.2 `BuildReasonHint`（生成诊断话术）

```go
func BuildReasonHint(correct, wrong, recentWrong int) string {
    total := correct + wrong
    if total == 0 {
        return ""
    }
    rate := float64(correct) / float64(total) * 100.0

    switch {
    case recentWrong >= 2:
        return fmt.Sprintf("近 3 题连错 %d 题，需重点排查", recentWrong)
    case rate < 50.0:
        return fmt.Sprintf("历史正确率仅 %.0f%%，基础不牢", rate)
    case rate < 60.0:
        return fmt.Sprintf("历史正确率 %.0f%%，刚跌破及格线", rate)
    default:
        return "掌握不稳定，需巩固"
    }
}
```

### 7.3 最近错题查询

```sql
-- latest_question_id 必须回查 questions（levels 无此信息）
SELECT q.id
FROM questions q
JOIN question_kps qkp ON qkp.question_id = q.id
JOIN exams e ON e.id = q.exam_id
WHERE e.kid_id = ? AND qkp.knowledge_point_id = ?
  AND e.status = 1
  AND q.is_correct = 0 AND q.status = 1
ORDER BY q.created_at DESC
LIMIT 1;
```

---

## 八、应用流程

### 8.1 触发时机（事件驱动 + 每日兜底）

```
题目对错变化（OCR 判完 / 家长改 / 孩子标）
   │
   ▼
入队 tasks(kind=1, detail={"kid_id":X})
   │
   ▼
Worker 消费 → 聚合 questions → levels → 派生 gaps
```

**另加**：每日维护兜底跑一次全量（防止任务丢失导致 `gaps` 陈旧）。

**允许重复入队**：Handler 全量重算幂等，不靠入队去重。

### 8.2 Worker 消费 `aggregate_gaps`（完整流程）

```
输入：kid_id

阶段 1：聚合 questions → levels（唯一真正的聚合）
  1. 反查 kids → exams → questions → question_kps → knowledge_points（**必带 `e.status = 1`**）
  2. 按 (kid_id, knowledge_point_id) 聚合：
     correct_count, wrong_count, recent_wrong_count, first/last_practiced_at
  3. level = CalculateLevel(...)
  4. UPSERT levels（全量重算，非增量）

阶段 2：levels → gaps（派生，不重复聚合）
  5. 取 levels 中 level=Weak 的 KP：
     对每个 KP：
       a. severity    = CalculateSeverity(levels.correct_count, levels.wrong_count, levels.recent_wrong_count)
       b. error_count = levels.wrong_count          ← 直接取，不重新数题
       c. reason_hint = BuildReasonHint(...)
       d. latest_question_id = 查最近错题（回查 questions）
       e. **查是否已有 ignored(3) 记录**：
          - 若存在 `gaps.status=3`（家长已忽略该 KP）→ **跳过**，不新建 active（"忽略即长期静默"）
          - 若已有 active(1) → UPDATE severity/error_count/latest_question_id/reason_hint
          - 若无 active 且无 ignored（首次/复发）→ INSERT status=1
  6. 取 levels 中 level IN (Good, Mastered) 的 KP：
     对每个已恢复的 KP：
       UPDATE gaps SET status=2, resolved_at=now
       WHERE kid_id=? AND knowledge_point_id=? AND status=1

```

**关键**：
- 阶段 1 是聚合，阶段 2 是筛选派生
- `error_count` 取自 `levels.wrong_count`，**不重新 `COUNT(questions)`**
- 全量重算，重跑安全
- **不用游标**：`aggregate_gaps` 按单 kid 全量重算，数据量极小；失败靠 `tasks.retry_count` 重试即可

### 8.3 家长手动忽略

```
PUT /api/gaps/:id {status: 3}
→ UPDATE gaps SET status=3, updated_at=now WHERE id=?
```

### 8.4 查询待攻克清单

```sql
SELECT g.*, kp.name AS kp_name, s.name AS subject_name
FROM gaps g
JOIN knowledge_points kp ON kp.id = g.knowledge_point_id
JOIN subjects s ON s.id = kp.subject_id
WHERE g.kid_id = ? AND g.status = 1
ORDER BY g.severity DESC, g.error_count DESC, g.created_at ASC;
```

### 8.5 查询历史（含已攻克）

```sql
SELECT g.*, kp.name AS kp_name
FROM gaps g
JOIN knowledge_points kp ON kp.id = g.knowledge_point_id
WHERE g.kid_id = ? AND g.status IN (1,2,3)
ORDER BY g.created_at DESC;
```

---

**软删对齐**：`latest_question_id` 外键 `ON DELETE SET NULL` 仅对**物理删除**生效；`questions` 日常软删（`status=2`）不触发外键。故：
- 事件驱动聚合（阶段 2）查最近错题时 `WHERE q.status=1`，已排除软删题；
- 若 gap 的 `latest_question_id` 指向一道被软删的题，由**每日全量兜底**重算纠正（`RecalculateGapsFull`）；
- UI 显示 `latest_question_id` 时应用层再校验 `questions.status=1`，否则显示"已删除"。

---

## 九、定时统计说明

| 问题 | 答案 |
|------|------|
| 需要定时吗？ | 需要，但是**事件驱动为主 + 每日兜底**，不是高频轮询 |
| 聚合几次？ | **一次**（`questions → levels`）；`gaps` 是派生，不重复聚合 |
| 触发源 | 题目对错变化 → 写 `tasks`；Worker 消费 |
| 失败怎么办？ | `tasks.retry_count` 重试（聚合数据量小，无需游标分批） |
| 幂等吗？ | 是，全量重算 UPSERT，重跑不翻倍 |
| 每日兜底 | 每日维护跑一次全量，防任务丢失导致陈旧 |

---

## 十、设计决策记录

| 决策 | 理由 |
|------|------|
| **保留 `gaps` 独立表** | 需状态流转、历史、人工忽略、诊断话术——`levels` 给不了 |
| **定位为 `levels` 派生** | 不重复聚合；`error_count` 直接取 `levels.wrong_count` |
| **无 `subject_id`** | 派生自 `knowledge_points.subject_id` |
| **`severity` 三档** | UI 颜色区分轻重 |
| **`latest_question_id` 回查 questions** | `levels` 无此信息；`ON DELETE SET NULL` 防孤儿 |
| **`reason_hint` 诊断话术** | 给家长看的人话 |
| **`status` 三态 + 部分唯一索引** | active/resolved/ignored；同孩同 KP 最多一条 active |
| **事件驱动 + 每日兜底** | 不高频轮询；防任务丢失 |
| **全量重算** | 重跑安全；非增量累加 |
| **三 `CASCADE` + 一 `SET NULL`** | 孩/KP 删 → 级联清；题删 → 最近错题置空 |
| **保留历史** | 攻克后置 resolved，支持产生→解决→复发 |

---

## 十一、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `kids` | `gaps.kid_id → kids.id` | 所属孩子（`ON DELETE CASCADE`） |
| `knowledge_points` | `gaps.knowledge_point_id → knowledge_points.id` | 薄弱知识点（`ON DELETE CASCADE`） |
| `subjects` | **无外键**；经 `knowledge_points.subject_id` 间接 | 学科（查询时 JOIN） |
| `questions` | `gaps.latest_question_id → questions.id` | 最近错题（`ON DELETE SET NULL`） |
| `levels` | 无外键；**派生源**（`level=Weak` 驱动） | 诊断输入 |
| `tasks` | 无外键；`kind=aggregate_gaps` 触发刷新 | 异步载体 |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 删 `kids` 行 | `CASCADE` 删其所有 `gaps` |
| 删 `knowledge_points` 行 | `CASCADE` 删其所有 `gaps` |
| 删 `questions` 行 | `SET NULL`（`latest_question_id` 置空，gap 保留） |
| 删 `subjects` 行 | `CASCADE` 删其下 `knowledge_points` → 间接 `CASCADE` 删 `gaps` |

---

## 十二、索引

```sql
CREATE INDEX idx_gaps_kid_status ON gaps(kid_id, status);
CREATE INDEX idx_gaps_kid_kp ON gaps(kid_id, knowledge_point_id);

CREATE UNIQUE INDEX uq_gaps_active_kid_kp
    ON gaps(kid_id, knowledge_point_id)
    WHERE status = 1;
```

**索引策略**：
- `(kid_id, status)` 服务"查某孩待攻克清单"（核心查询）
- `(kid_id, knowledge_point_id)` 服务"查某孩某 KP 的 gap 历史"
- `uq_gaps_active_kid_kp` 兜底防重复 active + 服务"查 active gap"
- 无 `subject_id` 索引（学科经 JOIN）

---


# `messages` 表设计（通知中心）

## 一、业务用途

给"人"看的通知中心：告知家长/孩子"系统做了什么、有什么待办"，**不阻塞任何流程**。

**核心场景**：
- 年级自动 +1 → 通知家长"已升级，如不符请修改"
- 备份完成/失败 → 通知家长
- AI 待审 N 条 → 通知家长
- 孩子端："你有一道错题待复习""本周攻克了 2 个薄弱点"

**边界**：
- **只增不改**（通知是历史，不编辑）
- **不阻塞流程**（grade 已经 +1 了，通知只是告知）
- **与 `tasks` 正交**：`tasks` = 系统要做的事（给 Worker）；`messages` = 人要知道的事（给人）
- **不用于沟通**（不是聊天，无回复）

---

## 二、关键设计问题：收件人怎么定？

拆成三个子问题：

### 2.1 收件人类型只有两种：家长 或 孩子

| 收件人 | 指向 | 说明 |
|--------|------|------|
| **家长** | 全体家长（`users` 表所有 active 行） | 家庭内家长权限一致，通知**不区分具体哪个家长** |
| **孩子** | 某个具体孩子（`kids.id`） | 每个孩子**只看自己的**通知 |

**不需要 `user_id`**——家长是"家庭级"的，所有家长看到同一份家长通知。这与你 `users` 表"家庭内家长权限一致"一致。

### 2.2 家长通知的"已读"怎么算？

**问题**：家长可能 1~3 个（爸爸、妈妈），一条家长通知，**爸爸读了，妈妈还没读**——算已读吗？

**两种模型**：

| 模型 | 说明 | 适用 |
|------|------|------|
| **A. 家庭级已读** | 任一家长读了 → 对全家已读 | 通知是"家庭事务"（如"备份完成"），一个家长知道就行 |
| **B. 每家长独立已读** | 每个家长有自己的已读状态 | 通知是"个人事务"（如"该你审核了"）——但家庭内不分你我 |

**推荐 A（家庭级已读）**：
- 符合"家庭内家长权限一致、不分你我"
- 简单：`messages.is_read` 一列搞定
- 爸爸读了，妈妈打开应用看不到未读红点——**可接受**（家庭事务一个家长知道就行）

**若未来要 B**：加 `message_reads(message_id, user_id)` 关联表。**当前不做**（YAGNI）。

### 2.3 孩子通知：一条孩子通知只归一个孩子

**关键**：`kids` 可能多条（多个孩子），所以孩子通知**必须绑定 `kid_id`**。

```
一条 messages 行：
  - 家长通知：kid_id = NULL, audience = parent
  - 孩子通知：kid_id = 5,    audience = kid
```

孩子打开自己端（`/kid/{code}`）→ 只查 `kid_id = 自己` 的通知。

---

## 三、字段定义

| 字段名 | 数据类型 | 约束条件 | 默认值 | 业务定义与规则说明 |
|--------|----------|----------|--------|---------------------|
| `id` | `INTEGER` | PK, AUTOINCREMENT | 无 | 通知唯一主键 |
| `audience` | `INTEGER` | NOT NULL | 无 | 受众：`1`=家长（全家）`2`=孩子 |
| `kid_id` | `INTEGER` | NULL, FK → `kids.id` | NULL | 孩子通知指向的孩子。`audience=2` 时必填；`audience=1` 时为 NULL。`ON DELETE CASCADE` |
| `kind` | `INTEGER` | NOT NULL | 无 | 通知类型（见枚举） |
| `title` | `TEXT` | NOT NULL | 无 | 通知标题 |
| `body` | `TEXT` | NULL | NULL | 通知正文（可选，纯文本） |
| `link` | `TEXT` | NULL | NULL | 跳转路径（如 `/exams/14`、`/gaps`），可空 |
| `is_read` | `INTEGER` | NOT NULL | `0` | 已读标记：`0`=未读 `1`=已读（家长通知为家庭级已读） |
| `read_at` | `DATETIME` | NULL | NULL | 首次标记已读时间 |
| `created_at` | `DATETIME` | NOT NULL | `CURRENT_TIMESTAMP` | 通知产生时间 |

---

## 四、建表 SQL

```sql
CREATE TABLE messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    audience    INTEGER NOT NULL,                 -- 1=parent 2=kid
    kid_id      INTEGER REFERENCES kids(id) ON DELETE CASCADE,
    kind        INTEGER NOT NULL,
    title       TEXT NOT NULL,
    body        TEXT,
    link        TEXT,
    is_read     INTEGER NOT NULL DEFAULT 0,
    read_at     DATETIME,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- audience=2（孩子）时 kid_id 必填；audience=1（家长）时 kid_id 必须为空
    CHECK (
        (audience = 1 AND kid_id IS NULL) OR
        (audience = 2 AND kid_id IS NOT NULL)
    )
);

CREATE INDEX idx_messages_parent ON messages(is_read, created_at DESC)
    WHERE audience = 1;
CREATE INDEX idx_messages_kid ON messages(kid_id, is_read, created_at DESC)
    WHERE audience = 2;
```

> **两条部分索引**分别服务"家长未读列表"和"某孩子的未读列表"——比一条联合索引更高效（家长通知 `kid_id` 为 NULL，孩子通知必带 `kid_id`）。

---

## 五、枚举值定义（Go 侧）

### `audience`

| 值 | 常量 | 含义 |
|----|------|------|
| 1 | `AudienceParent` | 家长（全家共享，`kid_id=NULL`） |
| 2 | `AudienceKid` | 孩子（指向具体 `kid_id`） |

### `kind`

| 值 | 常量 | 受众 | 含义 |
|----|------|------|------|
| 1 | `MessageKindGradeAdvanced` | 家长 | 年级已自动升级，如不符请修改 |
| 2 | `MessageKindBackupDone` | 家长 | 备份完成 |
| 3 | `MessageKindBackupFailed` | 家长 | 备份失败 |
| 4 | `MessageKindAIReviewPending` | 家长 | AI 待审 N 条 |
| 5 | `MessageKindStorageCleaned` | 家长 | 存储清理完成 |
| 6 | `MessageKindGapResolved` | 孩子 | 你攻克了一个薄弱点 |
| 7 | `MessageKindReviewDue` | 孩子 | 你有 N 道错题待复习 |
| 8 | `MessageKindGradeChanged` | 孩子 | 你的年级已更新 |
| 9 | `MessageKindGrade9Reminder` | 家长 | 升入初三提醒（生物 / 地理应已结课，可到科目管理隐藏） |
| 10 | `MessageKindGrade10Reminder` | 家长 | 升入高一提醒（可到孩子设置设置文理科） |
| 11 | `MessageKindGrade7Reminder` | 家长 | 升入初一提醒（科目变多，到孩子设置确认科目完整） |
| 99 | `MessageKindOther` | 家长 | 其他通知（兜底） |

**预留未启用**：`kind=5`（`MessageKindStorageCleaned`）、`kind=6`（`MessageKindGapResolved`）、`kind=7`（`MessageKindReviewDue`）、`kind=8`（`MessageKindGradeChanged`）**当前无写入方**，保留供未来扩展；新增通知类型时同步落地写入方。`kind=9`（`MessageKindGrade9Reminder`）由年级推进任务在目标年级为初三时写入；`kind=10`（`MessageKindGrade10Reminder`）在目标年级为高一（`raw_target=10`）时写入，link 指向 `/kids/<kid_id>`，提醒家长设置文理科（见 `tasks` 章与 `kids.track`）；`kind=11`（`MessageKindGrade7Reminder`）在目标年级为初一（`raw_target=7`）时写入，link 指向 `/kids/<kid_id>`，提醒家长确认科目完整。

**注意**：DB 无 CHECK 约束（`kind` 不设 CHECK），枚举校验在 service 层。加类型不改 schema。

---

## 六、字段在应用中的功能

| 字段 | 应用中的功能 |
|------|--------------|
| `id` | 主键 |
| `audience` | 决定查哪张列表；UI 区分家长端/孩子端 |
| `kid_id` | 孩子端按此过滤；家长端此列为 NULL |
| `kind` | UI 图标/分类；未来可筛选 |
| `title` | 列表标题；"年级已升级" |
| `body` | 详情正文（可选） |
| `link` | 点击跳转（如跳到该孩子档案、错题本） |
| `is_read` | 未读红点；列表默认显示未读 |
| `read_at` | 审计"何时读的" |
| `created_at` | 排序（新在前） |

---

## 七、应用流程

### 7.1 写通知（系统侧）

**家长通知**（如年级升级）：
```sql
INSERT INTO messages (audience, kid_id, kind, title, body, link)
VALUES (1, NULL, 1, '已自动升级到初二',
        '小明已从初一升到初二。如因休学/跳级不符，请到孩子档案修改。',
        '/kids/1');
```

**孩子通知**（示意；`kind=6`/`kind=7` 当前为预留，无落地写入方——见第五章）：
```sql
INSERT INTO messages (audience, kid_id, kind, title, body, link)
VALUES (2, 5, 6, '攻克了「二次函数」',
        '最近 3 题全对，正确率 90%，已标记为已掌握。',
        '/gaps');
```

### 7.2 读通知（家长端）

```sql
-- 家长未读列表
SELECT * FROM messages
WHERE audience = 1 AND is_read = 0
ORDER BY created_at DESC;
```

### 7.3 读通知（孩子端）

```sql
-- 某孩子的未读列表
SELECT * FROM messages
WHERE audience = 2 AND kid_id = ? AND is_read = 0
ORDER BY created_at DESC;
```

### 7.4 标记已读

**家长端**（家庭级，任一家长读即全读）：
```sql
UPDATE messages SET is_read = 1, read_at = CURRENT_TIMESTAMP
WHERE audience = 1 AND is_read = 0;
-- 或单条：WHERE id = ?
```

**孩子端**（只标自己的）：
```sql
UPDATE messages SET is_read = 1, read_at = CURRENT_TIMESTAMP
WHERE id = ? AND audience = 2 AND kid_id = ?;
-- ⚠️ 必须带 kid_id，防孩子 A 标孩子 B 的通知
```

> **安全要点**：孩子端标记已读必须校验 `kid_id = 当前孩子`，否则可越权标别人的通知。

### 7.5 年级流转（落地示例）

年级推进机制见 `kids` 表设计第五章「`grade` 自动推进机制」——入队 `tasks(kind=2, detail={"kid_id":X, "enrolled_year":Y, "enrolled_grade":Z, "target_grade":W})` → Handler 实时算目标年级并幂等赋值 → 派生本节通知。

**`messages` 侧写入（Handler 内，与推进同一次消费）**：
```
每个被推进的孩子写一条家长通知：
INSERT INTO messages (audience, kid_id, kind, title, body, link)
VALUES (1, NULL, 1, '已自动升级',
        '已自动升一级。如因休学/跳级不符，请到孩子档案修改。',
        '/kids/' || kid_id);
-- kind=1: MessageKindGradeAdvanced；audience=1 → kid_id 必为 NULL（CHECK 约束）
```

**要点**：
- **一童一条**——`link` 指向具体孩子档案，不把多个孩子聚合成一条
- **不阻塞**——`grade` 已由 Handler 赋值，通知只告知
- **不重复发**——Handler 幂等门为 `kids.grade == enrolled_grade`（见 `kids` 表第五章 step 3.c）；不成立即跳过更新、不发通知

**策略取舍**：自动推进（对 90% 场景正确）+ 通知告知（不阻塞）+ 家长可改。

---

## 八、设计决策记录

| 决策 | 理由 |
|------|------|
| **独立 `messages` 表** | 与 `tasks` 正交：`tasks`=系统要做的，`messages`=人要知道的 |
| **`audience` 两态（parent/kid）** | 家庭内只有两类人；家长不再分具体哪个 |
| **家长通知 `kid_id=NULL`** | 家长是家庭级；一条通知全家共享 |
| **孩子通知必带 `kid_id`** | 多孩场景；每个孩子只看自己的 |
| **`CHECK` 约束 `audience`/`kid_id` 配对** | DB 层兜底：家长通知不许带 kid_id，孩子通知必须带 |
| **家庭级已读（`is_read` 单列）** | 符合"家长不分你我"；爸爸读了全家已读 |
| **不做每家长独立已读** | YAGNI；家庭场景不需要；若未来要加 `message_reads` 关联表 |
| **`kind` 枚举无 CHECK** | 加类型不改 schema；service 层校验 |
| **`link` 可空** | 不是所有通知都要跳转 |
| **只增不改** | 通知是历史；无 update 语义（除 is_read） |
| **`ON DELETE CASCADE` on `kid_id`** | 删孩子 → 其通知自动清 |
| **两条部分索引** | 家长通知/孩子通知分别走各自的索引，比联合索引高效 |

---

## 九、与其他表的关联

| 关联表 | 关系 | 说明 |
|--------|------|------|
| `kids` | `messages.kid_id → kids.id` | 孩子通知的收件人（`ON DELETE CASCADE`） |
| `users` | **无外键** | 家长通知是家庭级，不指向具体 `users` |
| `tasks` | 无外键 | 两者正交：任务执行完可写一条 `messages` 作为"给家长的告知" |

**删除级联**：

| 触发 | 行为 |
|------|------|
| 删 `kids` 行 | `CASCADE` 删其所有孩子通知；家长通知（`kid_id=NULL`）保留 |
| 删 `users` 行 | 不影响 `messages`（家长通知不指向具体 user） |

---

## 十、索引

索引定义见第四章建表 SQL（`idx_messages_parent` / `idx_messages_kid` 两条部分索引，与 `CHECK` 约束同处 schema，**不重复声明**）。

**索引策略**：
- `idx_messages_parent` 部分索引服务"家长未读列表"（`audience=1`）
- `idx_messages_kid` 部分索引服务"某孩子的未读列表"（`audience=2`）
- 部分索引比全表联合索引小，且精确匹配两类查询
- `SQLite` 支持部分索引，`modernc.org/sqlite` 同样支持

---

## 十一、`messages` vs `tasks` 对照

| 维度 | `tasks` | `messages` |
|------|---------|------------|
| 给谁 | Worker（系统） | 人（家长/孩子） |
| 目的 | 系统要做的事 | 人要知道的事 |
| 消费 | Worker 拉取执行 | 人打开应用查看 |
| 状态 | pending/running/done/failed/skipped | is_read 0/1 |
| 生命周期 | 执行完清理 | 保留历史 |
| 是否阻塞 | 不阻塞用户 | 不阻塞任何流程 |
| 收件人 | 无（系统内部） | `audience` + `kid_id` |
| 失败处理 | 重试 | 无（通知发了就发了） |

**协同示例**：备份任务（`tasks`）执行完 → 写一条 `messages`（"备份完成"）给家长。





## 二、数据库物理分布与备份维护设计

### 2.1 本地资产文件系统分布规范

```
/data/paperly/
├── paperly.db                           # 主数据库文件 (WAL 模式)
├── paperly.db-wal                       # WAL 日志写入缓存
├── paperly.db-shm                       # 共享内存索引
├── assets/
│   ├── exams/                           # 试卷资产（卷内绑定）
│   │   └── {exam_id}/
│   │       ├── page_001.webp            # 试卷页原图
│   │       ├── page_002.webp
│   │       └── questions/               # 该卷切出的题图
│   │           ├── q_001.webp
│   │           └── q_002.webp
│   ├── avatars/                         # 孩子头像
│   └── tmp/                             # 重排页暂存工作区（assets/tmp/{token}/，任务结束即清）
├── seed/                                # 种子数据
└── backup/                              # 分级快照（daily 7 份 / weekly 4 份 / monthly 12 份）
    ├── daily/
    ├── weekly/
    └── monthly/
```

**设计原则**：**卷内强绑定**——一份试卷的全部资产（页图 + 题图）落在 `assets/exams/{exam_id}/` 下，**删除即删目录**。放弃跨卷全局 sha256 去重，避免 SQLite 事务无法覆盖文件 I/O 导致的误删/孤儿问题。

### 2.2 迁移与维护机制

1. **版本读取 SQL 兼容防御**：

   ```
   -- 外层包夹子查询，防止空表返回空集，恒定输出单行 'unknown' 或版本字符串
   SELECT COALESCE((SELECT display_tag FROM migrations ORDER BY version DESC LIMIT 1), 'unknown') AS display_tag;
   
   ```

2. **每日维护（`DailyMaintenance`）**：

   * **不依赖游标**——家庭场景数据量小，按 kid 全量重算即可（与 `aggregate_gaps` 口径一致，失败靠 `tasks.retry_count` 重试）。

   * 执行纯内存指标重算；**仅对统计值实际变化的 `levels` 行**在事务末尾刷新 `updated_at`——**不做全表 touch**（保留 `updated_at` 的"该行确实变过"审计语义），另写 4 个 gauge 快照（db/assets/fs_free/fs_total），并按 is_gauge 分档清理（counter 90 天 / gauge 365 天）；见 `REQUIREMENTS.md` 每日维护章节。
