# Paperly 需求文档

> **版本**：v1.0.0
> **最后更新**：2026-10-10
> **权威性**：本文档是产品需求 / 用户流程 / 业务规则 / 验收标准的权威源。
> **去重原则**：数据库 schema 见 `DATABASE_DESIGN.md`；技术栈见 `TECH_STACK.md`；API 见 `API.md`；前端路由见 `ROUTES.md`。

> 战略：**Box 优先，SQLite（modernc.org/sqlite），无多租户**。
> 品牌：**Paperly** ｜ Slogan：**Scan. Score. Succeed.**

---

## 一、项目概述

### 1.1 项目背景

为初中**全学科（语文、数学、英语、物理、化学、道德与法治）**试卷管理设计一套轻量、高效的本地化系统。核心目标：**上传即结构化、薄弱点可诊断、知识点掌握度可度量、家长可审核**。

### 1.2 产品线

| 产品线 | 形态 | 数据 | 本版 |
|--------|------|------|------|
| **Paperly Box** | Self-Host，Docker | SQLite（modernc.org/sqlite）+ 文件系统，无多租户 | ✅ 本版完成 |
| **Paperly Cloud** | SaaS，月付 | 待定（见第十四章） | ⏸ 暂缓 |

### 1.3 核心设计原则（Box 版）

| 原则 | 说明 |
|------|------|
| 极简架构 | 无多租户，单机构单实例 |
| 单一权威源 | 每个事实只有一个权威存储地，一条写入链 |
| **聚合幂等** | `levels` / `gaps` 只走**全量重算**（从 `questions` 重数 + UPSERT），禁止增量累加 |
| **枚举存储** | DB 存 INT / 英文枚举；中文只在 DTO 层映射（AGENTS.md 规范） |
| 计算态/存储态分离 | 落库字段与实时计算字段边界清晰 |
| 单机务实 | 跑不通的机制直接砍 |
| **家庭边界** | 实例即家庭、单实例单家庭。**只排除**教师端、班级管理、家校沟通、学校账号体系；**不排除**孩子作为独立用户、独立设备、自主使用。家长=管理员与最终审核者，孩子是一等用户（非只读，可看/可反馈/可做复习），AI=助手 |
| **单实例单家庭（不可动摇的前提）** | 本版所有设计决策以「单实例单家庭」为前提。不预留多租户、不预留跨家庭、不预留 SaaS 迁移路径。遇到「要不要为多租户 / SaaS / 跨家庭 / 多设备同步预留」的问题，答案一律是**不预留**，直接按单家庭场景做最简设计。若未来做 SaaS，是一次重新设计，不是当前设计的「扩展」。 |

> **家庭边界声明**：Paperly Box **实例即家庭、单实例单家庭**——不是多家庭平台，也不是几家搭伙。**不提供**教师端、家校沟通、班级管理、学校账号体系；但**不排除**孩子作为独立用户、独立设备、自主使用。系统不是家长监控工具，而是**家庭学习协作系统**：家长是管理员与最终审核者，孩子是**一等用户**（可看、可反馈、可做复习，非只读），AI 只负责提建议。所有诊断（错题图片诊断、试卷失败归因）、报告、能力雷达**仅供本家庭内部使用，不对外共享、不与学校/教师发生任何数据交换，也不跨家庭比较**。
>
> **本声明是顶层原则，不是「暂不支持」清单**——第十四章 Cloud 版差异标注为「已排除」，当前版本不为其预留任何设计空间。

---

## 二、前置关卡：SQLite PoC（已完成，结果见 ADR 0001）

### 2.1 验证项（SQLite 版，附 Day 2 实测）

| # | 验证项 | 通过标准 | Day 2 结果 | 不通过应对 |
|---|--------|----------|-----------|------------|
| 1 | SQL 方言 | DDL/INSERT/SELECT 语法可用 | ✅ PASS | 调整 `migrations/0001_init.sql` |
| 2 | 自增主键 | `INTEGER PRIMARY KEY AUTOINCREMENT` 自增、不复用旧 id | ✅ PASS | 应用层生成 |
| 3 | 复合唯一约束 | `UNIQUE(a,b,c)` 冲突报错 | ✅ PASS | 应用层校验 |
| 4 | 索引 | `EXPLAIN QUERY PLAN` 显示 `USING INDEX` | ✅ PASS | 查询性能无保障 |
| 5 | JSON 函数 | `json_each` / `JSON_EXTRACT` 可用；数组元素取 `$[0]` | ✅ PASS | — |
| 6 | 单文件并发 | 多读单写；无 `SQLITE_BUSY` 错误 | ✅ PASS：默认池并发写报 BUSY；写池 `SetMaxOpenConns(1)` + `busy_timeout(5000)` + `_txlock=immediate` → 0 错误 | 写池串行化 |
| 7 | 备份 | 在线备份不停机；一致性验证通过 | ✅ PASS：`VACUUM INTO` 在线；`PRAGMA integrity_check` = ok | 停机复制 |
| 8 | 单文件上限 | ≥ 2GB | ✅ PASS：实测 1.01GB 无异常 | 分库 |
| 9 | 乐观版本号 | `UPDATE ... WHERE id=? AND row_version=?` 行为正确 | ✅ PASS：success=1 / conflict=1 / busy=0 | 全量重算停机 |

> **PoC 结论**：9 项全部 PASS。
>
> **PoC 已通过，编码阶段已启动**——Day 2 已完成 PoC、ADR 0001。

> **补充**：「**写池串行**」（单连接 + `_txlock=immediate` + `busy_timeout`，ADR 0001）是**应用层单连接池机制，不依赖数据库引擎能力，故不在 PoC 验证范围**——其正确性由应用层单元测试保证。上表 PoC 只需验证数据库是否提供"乐观版本号所需的事务能力"（验证项 9）即可，评审不必再抓"写池串行未验证"。

### 2.2 PoC 结论（已定）

```
PoC 通过
```

---

## 三、数据库设计（Box 版）

> 无多租户，**所有表不含 `tenant_id`**。
>
> **权威源**：`DATABASE_DESIGN.md`（16 表）。本章为需求侧摘要，字段级细节
> （索引理由、设计决策记录、完整流程图）以 `DATABASE_DESIGN.md`
> 为准；**两者冲突时以 `DATABASE_DESIGN.md` 为准**。
>
### 3.1 权威表清单（16 表）

全部 16 表由单脚本 `migrations/0001_init.sql` 一次建齐（幂等，见 8.1 节）。
「点亮阶段」列的功能排期口径见第十章；「代码现状」以 `internal/domain` + `internal/repository`
实际文件为准。

#### 组 A：核心业务表（10）

| # | 表名 | 业务用途 | 点亮阶段 | 代码现状 |
|---|------|----------|----------|----------|
| 1 | `users` | 家长账号 + PIN 登录 | P0-MVP | 已实现 |
| 2 | `kids` | 孩子档案 + 免登录 access_code | P0-MVP | 已实现 |
| 3 | `exams` | 试卷实体（元信息） | P0-MVP | 已实现 |
| 4 | `pages` | 试卷页（一页一行） | P0-MVP | 已实现 |
| 5 | `subjects` | 学科字典 | P0-MVP | 已实现 |
| 6 | `kid_subjets` | 孩子学科映射表 | P1 | 已实现 |
| 7 | `knowledge_points` | 知识点字典 | P1 | 已实现 |
| 8 | `questions` | 题目（含题面 + 答案 + 对错 + 审核） | P1 | 表已建 / 无读写方 |
| 9 | `question_kps` | 题目↔知识点多对多 | P1 | 表已建 / 无读写方 |
| 10 | `levels` | 孩子×知识点 掌握水平 | P1 | 表已建 / 无读写方 |
| 11 | `gaps` | 薄弱点与待攻克清单 | P1 | 表已建 / 无读写方 |

#### 组 B：平台表（2）

| # | 表名 | 业务用途 | 点亮阶段 | 代码现状 |
|---|------|----------|----------|----------|
| 12 | `tasks` | 事件驱动的待办队列（Worker 消费） | P1 | 已实现 |
| 13 | `messages` | 通知中心（家长 / 孩子收件箱） | P1 | 已实现 |

#### 组 C：运维表（3）

| # | 表名 | 业务用途 | 点亮阶段 | 代码现状 |
|---|------|----------|----------|----------|
| 14 | `migrations` | 迁移历史 + 版本标签 | P0-地基 | 已实现 |
| 15 | `metrics` | 运维指标日聚合 | P0-地基 | 已实现 |
| 16 | `cursors` | 大任务断点游标 | P1 | 已实现 |

> **三表职责边界**（易混，权威见 `DATABASE_DESIGN.md` `tasks` 表第十一章）：
> `tasks` = 「有事要做」（事件队列，短生命周期，Worker 拉取执行）；
> `cursors` = 「大任务跑到哪」（断点位置，长生命周期，持续更新）；
> `metrics` = 「当日发生多少次」（观测聚合，中生命周期，保留 90 天）。
> 三者协同：事件 → `tasks` 入队；大任务 → `cursors` 记断点；执行 → `metrics` 打点。

### 3.2 全局口径

本节固化全文通用的存储与算法口径；各表细节见 3.3–3.17 节。

| 维度 | 口径 |
|------|------|
| 学科引用 | `subject_id` INTEGER 外键 → `subjects`，**不用中文名文本** |
| 年级 | `INTEGER` **1~12**（CHECK 约束），不用「初一/初二/初三」文本 |
| 文理分科 | `kids.track` `0/1/2`（未分科 / 文 / 理）；`subjects.mask` 高 2 位承载文理位；**仅 `grade ≥ 10` 参与过滤**，`grade < 10` 时 track 强制视为 0 |
| 枚举存储 | 状态 / 类型列绝大多数为 `INTEGER`（枚举在 service 层校验）；文本枚举仅 `subjects.code` 等稳定程序标识 |
| 掌握水平 | `levels.level` **四档整数**（`CalculateLevel`，见 3.11 节），无 0-100 连续分 |
| 难度 | **无难度维度**——只判对错，不存难度 |
| 分类体系 | 学科 / 知识点为**两张独立字典表**（见 3.7 / 3.8 节），无标签共现表 |
| 分数 | **无分数**——`questions.is_correct` 布尔 |
| 知识点关系 | **无关系边表**，层级固定「学科 → 知识点」两级，故无传播类算法 |
| 审核载体 | 审核状态**内联**在 `questions.review_status`，无独立审核队列表 |
| 异步载体 | 事件队列 `tasks`（见 3.13 节），断点与一次性标记 `cursors`（见 3.17 节），二者不合并 |

### 3.3 `users` 家长账号

表结构 / 字段 / 索引见 `DATABASE_DESIGN.md` `users` 表章节。

- **`pin_version`**：改 PIN 时自增，用于**踢下线**——客户端携带的 `pin_version` 与库中不符即强制重新登录。
- **`status`**：家长不参与「毕业」态，仅 1 - 正常 / 0 - 停用。
- **`display_name`**：家长昵称，`UNIQUE` 保证单机单账号可辨识。
- 无 `tenant_id`；无密码找回（Box 单机，见 `DATABASE_DESIGN.md` `users` 表第八章）。

### 3.4 `kids` 孩子档案

表结构 / 字段 / 索引见 `DATABASE_DESIGN.md` `kids` 表章节。

- **`access_code`**：孩子端**免登录凭据**，`UNIQUE`；`/kid/{access_code}` 即孩子入口。
  后端**永不返回** `access_code` / `pin_hash`（仅创建时一次性返回，见第十一章）。
- **`grade` 自动推进**：每年 9 月 1 日由 `tasks`（`TaskKindAdvanceGrade`）推进；
  推进后发 `messages` 通知家长确认（见 3.13 / 3.14 节）；升入初一（目标 `grade=7`）时
  额外发一条提醒（`MessageKindGrade7Reminder`，链接 `/kids/<id>`），提示到孩子设置确认科目完整；
  升入初三（目标 `grade=9`）时
  额外发一条提醒（`MessageKindGrade9Reminder`），提示生物 / 地理已结课、可到科目管理按学段隐藏；
  升入高一（目标 `grade=10`）时追加一条提醒（`MessageKindGrade10Reminder`，链接 `/kids/<id>`），
  提示可到孩子设置设置文理科。
- **`track`（文理分科）**：`0` 未分科 / `1` 文科 / `2` 理科。**仅高中（`grade ≥ 10`）参与科目过滤**，
  初中及以下即使已存也**不生效**（`GET /api/subjects` 与前端 `trackBitFor` 一致强制：`grade < 10` 时 track 视为 0）；
  驱动 `subjects.mask` 高 2 位过滤（上传页 / 雷达图）。家长可全程保存 track，只是到高中后才起作用。

### 3.5 `exams` 试卷实体

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `exams` 表章节。

**核心语义**：`exams` 是**试卷元信息表**（一行一份卷），页文件在 `pages`（3.6 节）。
**无 `title` 必填**、无 `sha256`（哈希下沉到 `pages`）、无 `ocr_status`（OCR 阶段再评估）。

**「上传者」与「归属」是两个事实**：

- `uploaded_by` = **谁传的**（审计事实，`INTEGER` 枚举，不可改；1=家长 / 2=孩子，**不记录具体哪个家长**）
- `kid_id` = **归谁**（业务归属，可由家长随时改；`NULL` = 未归属）

家长代传的卷子归孩子所有，孩子在自己详情页可见；反之家长可把未归属卷子认领给自己孩子。

### 3.6 `pages` 试卷页

表结构 / 字段 / 索引见 `DATABASE_DESIGN.md` `pages` 表章节。

- **一页一行**：`UNIQUE(exam_id, page_index)` 保证页序唯一。
- **`sha256` 仅用于完整性校验**，**不做跨卷去重**——放弃全局引用计数，改为**卷内强绑定目录**
  `assets/exams/{exam_id}/`，删除试卷即删目录（见 3.18 节）。理由见 `DATABASE_DESIGN.md` `pages` 表第四章。
- 上传为**一次多文件表单提交**：1~50 页（上限为单文件体积保护，见第七章）。
- **无独立 service / handler / 路由**——`pages` 只由试卷上传与详情查询触碰。

### 3.7 `subjects` 学科字典

表结构 / 字段 / 索引见 `DATABASE_DESIGN.md` `subjects` 表章节。

- **`code`**：英文代码，稳定引用（不随改名而变）。
- **`source`**：`1 = SubjectSourceSystem`（系统预置）/ `2 = SubjectSourceUser`（用户自定义）。
- **`mask`**：**5 bit 可见位**——低 3 位=学段（1 小学 / 2 初中 / 4 高中），高 2 位=文理（8 文 / 16 理），默认 `31` 全开。上传页与雷达图按孩子**年级 + 文理**过滤学科；文理维仅 `grade ≥ 10` 生效（`grade < 10` 时 track 强制视为 0）。家长可在科目管理修改（0~31）。
- **`mandatory`**：`1`=必选科（语文 / 数学），孩子在科目白名单中不可取消；由 seed 固定，API 不可改。
- **`kid_subjects`**：`kid × subject` 集合（见 `DATABASE_DESIGN.md` `kid_subjects` 表章节）。孩子相关科目一律只读此表，**无 mask 回退**，无记录即空。消费方：上传页科目下拉、雷达图科目轴（`GET /api/subjects?kid_id=N`）、试卷分布（`GET /api/dashboard/distribution?kid_id=N`）。
- **预置 13 科**：`internal/seed/data/subjects.json`，首启经 `cursors` 幂等导入（见 8.5 节）。
- 供 `GET /api/subjects`（可选 `?grade=N&track=T` 双维过滤，或 `?kid_id=N` 按孩子白名单）列出；用户可新增自定义学科（`source=2`）。

### 3.8 `knowledge_points` 知识点字典

表结构 / 字段 / 索引见 `DATABASE_DESIGN.md` `knowledge_points` 表章节。

- **两级固定**：学科 → 知识点，归属由 `subject_id` 外键表达（不再用字符串 `parent_tag`）。
- **无 `code`**：知识点数量多，不设英文代码，靠 `(subject_id, name)` 唯一。
- **无别名 / 无 `usage_count`**：后者派生自 `question_kps` 计数。
- **无关系边**：知识点之间无「前置 / 后继」关系，因此无传播类算法。

### 3.9 `questions` 题目

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `questions` 表章节。

**题面 + 作答 + 对错 + 审核合一行**，无独立答案表与分数表。
**无 `score` / 无 `difficulty`**——只判对错；**无 `options` 列**——选项并入 `content`。

> **枚举校验在 service 层**，DB 不设 CHECK 约束。
> **AI 产出默认 `review_status=0`**（未审核），仅 `confirmed` 才进 `levels` / `gaps` 闭环。
> 审核痕迹落 `reviewed_by` / `reviewed_by_type` / `reviewed_at` / `review_note`。

### 3.10 `question_kps` 题目↔知识点关联

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `question_kps` 表章节。

- **多对多**，`UNIQUE(question_id, knowledge_point_id)` 去重。
- **`is_primary`**：主次二分（每题最多一个主知识点，service 层保证）。
- **`source`**：1 = AI 自动建立 / 2 = 家长手动调整。
- 双 `CASCADE`：删题或删知识点，关联行自动清。
- **无权重值**——只留主次二分，无难度加权（无难度维度，故不需要权重）。

### 3.11 `levels` 孩子×知识点 掌握水平

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `levels` 表章节。

**无 `subject_id`**（派生自 `knowledge_points.subject_id`）、**无 `total_count`**（= `correct_count + wrong_count`）、
**无 `accuracy_rate`**（查询时算）。**无连续掌握分、无衰减因子、无独立难度维度**——
只有四档整数 `level`。

**水平判定规则**（`CalculateLevel` 纯函数，输入做对数 / 做错数 / 最近 3 题错数；实现见 `DATABASE_DESIGN.md` `levels` 表第五章）：

| 条件 | 判定 |
|------|------|
| `total < 3` | `LevelUntrained`（样本不足） |
| `total >= 3` 且正确率 `< 60%` | `LevelWeak` |
| `total >= 3` 且 `recent_wrong_count >= 2` | `LevelWeak` |
| `total >= 5`、正确率 `>= 85%` 且最近 3 题全对 | `LevelMastered` |
| 其他 | `LevelGood` |

### 3.12 `gaps` 薄弱点与待攻克清单

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `gaps` 表章节。

- **`gaps` 与 `levels` 的分工**：`levels` = **某知识点做得怎么样**（水平）；
  `gaps` = **需要攻克什么**（待办清单 + 诊断话术）。
- **`error_count` 取自 `levels.wrong_count`**（同源，不独立计数）。
- **部分唯一索引**保证同一孩子同一知识点**最多一条 active**，避免重复入清单。
- **无 `subject_id`**（派生）。

### 3.13 `tasks` 待办队列

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `tasks` 表章节。

- **允许重复入队 + Handler 幂等**：`UNIQUE` 只约束业务键，不加在队列上。
- **`detail`**：JSON 载荷（默认 `'{}'`）；任务类型与载荷示例见 `DATABASE_DESIGN.md` `tasks` 表第四章。
- **与 `cursors` 的区别**：`tasks` 管「单次事件」，`cursors` 管「大任务断点」——
  短任务用 `tasks`，需断点续跑的大批量任务用 `cursors`（见 3.17 节）。
### 3.14 `messages` 通知中心

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `messages` 表章节。

**收件人只有两种**（CHECK 强制）：家长（`kid_id` 必为 `NULL`，全家共享）或孩子（`kid_id` 必填）。
两条**部分索引**分别服务「家长未读列表」与「某孩子的未读列表」——比一条联合索引更高效。

> **`kind` 不设 CHECK**，枚举校验在 service 层——加通知类型不改 schema。
> **预留未启用项当前无写入方**，保留供未来扩展；新增通知类型时**必须同步落地写入方**。
>
> **`messages` vs `tasks`**：给 Worker 的事走 `tasks`，给人看的事走 `messages`。
> 协同示例：备份任务（`tasks` `kind=3`）执行完 → 写一条 `messages`（`kind=2`）给家长。

### 3.15 `migrations` 迁移记录

表结构 / 字段 / 索引见 `DATABASE_DESIGN.md` `migrations` 表章节。

- **`name`**：记录具体哪个迁移脚本，强调「迁移历史」而非「版本号」。
- 迁移在**单事务内**幂等执行；体检页读最新 `display_tag`（见 8.1 节）。

### 3.16 `metrics` 运维指标日聚合

表结构 / 字段 / 索引 / 读写口径见 `DATABASE_DESIGN.md` `metrics` 表章节。

- **写路径**：单次 upsert（`ON CONFLICT(metric_name, metric_date)`），跨进程重启持久。
- **读路径**（体检页 8.6 节）：近 7 天 `SUM(count)`，`COALESCE` 兜底 0。
- **`is_gauge` 语义**：0 = counter（当日累计，读 `SUM`）/ 1 = gauge（当日快照，读最新值）——读侧拿到一行即知口径，不用为每个指标名写分支。
- **保留策略分档**：每日维护顺带清理，counter 留 90 天、gauge 留 365 天。

### 3.17 `cursors` 任务游标

表结构 / 字段 / 索引 / 枚举见 `DATABASE_DESIGN.md` `cursors` 表章节。

- **类型为 `INT`**，与全局枚举规范一致。
- **两种用法**：断点续跑（大任务分批，记 `last_id`）／ 一次性标记（seed 导入，记完成位）。
- **首启 seed 导入靠 `cursors` 幂等**（见 8.5 节）。

### 3.18 本地资产文件系统布局

完整布局见 `DATABASE_DESIGN.md` 第二章 2.1 节。

- **卷内强绑定**：一份试卷的全部资产落在 `assets/exams/{exam_id}/` 下，**删卷即删目录**。
- **大字段落文件系统，DB 只存路径**：`pages.file_path` / `questions.image_path`。
- **无全局哈希目录**：OCR 产物落卷内 `questions/`。
- **备份分级与保留份数**：见 `DATABASE_DESIGN.md` 第二章 2.1 节（分级快照）。

### 3.19 存储 / 级联 / 软删

#### 状态语义

| 表 | status 取值 | 读路径过滤 |
|----|------------|------------|
| `kids` | 1 / 2 / 3  | 家长端默认 `status=1`；含毕业只读视图时 `IN (1,2)` |
| `users` | 1 / 0 | `status=1` |
| `exams` | `1` 正常 / `2` 软删 | 列表过滤 `status=1` |
| `questions` | `1` 正常 / `2` 软删 | 查询过滤 `status=1` |
| `gaps` | `1` 待攻克 / `2` 已攻克 / `3` 已忽略 | 待办清单过滤 `status=1` |

> **三态（1 /2 /3）仅适用于 `kids`**——唯一有「毕业」语义的主数据表。
> `users` 无毕业态；`exams` / `questions` 只有正常 + 软删两态；
> `gaps.status` 是攻克状态机（待攻克/已攻克/已忽略），**不是软删**——
> 用户「删除」薄弱点应置 `Ignored` 而非软删。
> `tasks.status` / `cursors.last_status` 是执行状态机，与可见性无关。

#### 级联删除

级联链见 `DATABASE_DESIGN.md` 各表"与其他表的关联"节；原则：孩子清则水平/薄弱/孩子通知随之清、试卷转未归属；
删卷则页/题/关联随之清、题图引用置空、**删目录 `assets/exams/{exam_id}/`**；
删学科则知识点及下游随之清、试卷学科置空。

> **硬删两阶段**（`DATABASE_DESIGN.md` `exams` 表第六章 6.6 节）：先软删（`status=2`，列表消失、DB 保留）
> → 确认后硬删（DB `CASCADE` + 删目录）。回收站视图 = 软删集合（`exams` 表 6.7 节）。

## 四、核心功能

### 4.1 试卷上传（多页）

用户在上传页**一次性提交**：元数据（学科 / 年级 / 考试类型 / 日期 / 归属 / 上传者）+ **N 张图片**（1~50 页）。

**上传者与归属是两个独立事实**（见 3.5 节）：

| 入口 | `uploaded_by` | `kid_id` |
|------|---------------|----------|
| 家长端 | `1`（parent，审计，不可改） | 家长选择，可空（未归属） |
| 孩子端 | `2`（kid） | **强制为该孩子** |

**后端三段式**（异构存储无法原子，故拆三段 + 兜底）：

```
阶段 1（事务内，秒级）：INSERT exams(...) → exam_id
阶段 2（事务外，不持锁）：mkdir assets/exams/{exam_id}/
                        对每张图算完整 SHA256（仅完整性记录）
                        写 page_{index:03d}.{ext}
阶段 3a（事务内）：INSERT pages(exam_id, page_index, file_path, sha256) × N → 返回 exam_id
阶段 3b（失败）：RemoveAll(dir) + DELETE exams WHERE id=exam_id → 返回错误
```

- **落盘必须在事务外**：N 张图可能耗时数秒，放进事务会让写池长期持锁、阻塞所有其他写操作。
- **崩溃残留兜底**：列表查询加 `EXISTS (SELECT 1 FROM pages WHERE exam_id = e.id)`，
  隐藏「有 exam 行、无 pages 行」的孤儿中间态；未来加 `cleanup_orphan_exams` 任务扫尾。
- **`sha256` 不做跨卷去重**——卷内强绑定目录，删卷即删目录（见 3.6 / 3.18 节）。

### 4.2 题目切分与入库

**一次 OCR + AI 产出入库**（入库流程见 `DATABASE_DESIGN.md` `questions` 表第六章 6.1 节）：

```
1. 试卷 OCR 完成 → 从页图裁切出 N 道题
2. 对每道题：
   a. 裁切题目区域 → 落盘 assets/exams/{exam_id}/questions/q_{no:03d}.webp
   b. INSERT questions(exam_id, page_id, question_no, question_type,
                       content, image_path, image_sha256,
                       student_answer, correct_answer, is_correct,
                       content_source=1, answer_source=1, is_correct_source=1,
                       review_status=0, status=1)
3. AI 打知识点 → INSERT question_kps(question_id, knowledge_point_id,
                                       is_primary, source=1)
4. 入队 tasks(kind=1, detail={"kid_id":X}) → 触发 levels / gaps 聚合（4.5 / 4.6 节）
```

- **`review_status=0`（pending）**：AI 产出默认未审核，**不进闭环**——只有家长 / 孩子
  确认（`review_status=1`）后才被 4.5 节聚合消费（见 4.7 节）。
- **`page_id` 溯源**：题目定位到具体页，便于「切错在哪一页」定位与重切。
- **OCR 未配置时降级**：无 OCR Provider 则跳过切分，走「空卷 + 手工录题」路径，
  核心上传功能仍可用（见第七章降级要求）。

### 4.3 答案录入与逐题校对

校对界面强制**「题面 + 题图 + 学生答案 + AI 答案 + AI 判对错」同屏逐题绑定**，

- **题面与选项同存 `content`**（Markdown + LaTeX），无 `options` 列。
- **一题一图**：`image_path` + `image_sha256`（取代旧 JSON 数组）。
- **学科一致性提示**：加载校对页时比对本题所有 `question_kps` 关联 KP 的
  `knowledge_points.subject_id` 与所属 `exams.subject_id`；不一致则页面顶部提示
  「该题知识点学科与试卷学科不符，请审核」，由人工确认 KP 归属。
- **`UNIQUE(exam_id, question_no)`**：题号在一份卷内唯一，重切走 4.12 节替换协议。

### 4.4 学科与知识点筛选

结构化列直接筛选，无 JSON 标签、无共现表：

**卷级筛选**（索引口径见 `DATABASE_DESIGN.md` `exams` 表第九章，全部走 B-tree 索引）：

```
GET /api/exams?subject_id=2&grade=9&exam_type=5&kid_id=1&page=1&page_size=20
```

| 维度 | 列 | 索引 |
|------|----|------|
| 学科 | `exams.subject_id` | `idx_exams_subject` |
| 年级 | `exams.grade`（INT 1~12） | `idx_exams_grade` |
| 考试类型 | `exams.exam_type` | `idx_exams_exam_type` |
| 归属孩子 | `exams.kid_id` | `idx_exams_kid` |
| 日期排序 | `exams.exam_date` | `idx_exams_exam_date` |

**知识点级筛选**（走 `question_kps` / `knowledge_points` 反查题目；实现见 `DATABASE_DESIGN.md` `question_kps` 表第六章 6.3 节）：

- **年级 / 学科是结构化列，不是标签**——`grade` 有 `CHECK (BETWEEN 1 AND 12)`，
  `subject_id` 有外键，**无脏值可能**。

### 4.5 掌握度聚合与水平判定

**唯一聚合载体**：`levels`（孩子 × 知识点）。**全量重算，不是增量累加**。

**触发时机**（见 `DATABASE_DESIGN.md` `gaps` 表第八章 8.1 节）：题目对错变化（OCR 判完 / 家长改 / 孩子标）即入队
`tasks(kind=1)`；另有每日维护兜底跑一次全量（防任务丢失导致数据陈旧）。

**聚合流程**（见 `DATABASE_DESIGN.md` `levels` 表第七章 7.1 节，Worker 消费 `aggregate_gaps` 阶段 1）：

```
1. 反查（**必带 e.status = 1**）：kids → exams → questions → question_kps → knowledge_points
2. 按 (kid_id, knowledge_point_id) 聚合：
     correct_count      = COUNT(questions WHERE is_correct=1)
     wrong_count        = COUNT(questions WHERE is_correct=0)
     recent_wrong_count = 最近 3 题中 is_correct=0 的数量
     first_practiced_at = MIN(exams.exam_date / questions.created_at)
     last_practiced_at  = MAX(exams.exam_date / questions.created_at)
3. level = CalculateLevel(correct_count, wrong_count, recent_wrong_count)
4. UPSERT levels（ON CONFLICT(kid_id, knowledge_point_id) DO UPDATE）
   —— first_practiced_at 用 COALESCE(levels.first_practiced_at, excluded.…) 保留首次值
```

> **全量重算是硬要求**：若用 `correct_count += 1` 增量，重跑任务会**翻倍**。
> 唯一权威链 = 从 `questions` 重数。

**水平判定**：`CalculateLevel` 纯函数（口径见 3.11 节）——
`total<3` 未训练 / 正确率`<60%` 或近 3 题错`≥2` 薄弱 / `total≥5` 且`≥85%` 且近 3 题全对 已掌握 / 其余 良好。

**只读查询**（知识树 / 正确率查询实现见 `DATABASE_DESIGN.md` `levels` 表第七章 7.2–7.4 节；`levels` 不存 `subject_id` 与正确率列）。

### 4.6 薄弱点派生与攻克

**`levels`（水平）与 `gaps`（待办）分工**：`levels` 答「某知识点做得怎么样」，
`gaps` 答「接下来攻克什么」+ 诊断话术。

**派生流程**（见 `DATABASE_DESIGN.md` `gaps` 表第八章 8.2 节，Worker 阶段 2——**派生，不重复聚合**）：

```
5. 取 levels 中 level=Weak 的 KP，逐个：
     a. severity         = CalculateSeverity(correct, wrong, recentWrong)
     b. error_count      = levels.wrong_count            ← 直接取，不重数题
     c. reason_hint      = BuildReasonHint(correct, wrong, recentWrong)
     d. latest_question_id = 回查最近错题（JOIN questions WHERE q.status=1）
     e. 查已有记录：
        · 存在 status=3（家长已忽略）→ **跳过**，不新建 active（"忽略即长期静默"）
        · 存在 status=1            → UPDATE severity/error_count/latest/reason
        · 无 active 且无 ignored    → INSERT status=1
6. 取 levels 中 level IN (Good, Mastered) 的已恢复 KP：
     UPDATE gaps SET status=2, resolved_at=now
     WHERE kid_id=? AND knowledge_point_id=? AND status=1
```

**纯函数**（与 `CalculateLevel` 同源输入，实现见 `DATABASE_DESIGN.md` `gaps` 表第七章 7.1 / 7.2 节；
判定口径：总数为 0 取轻度；正确率 `<50%` 或近 3 题错 `≥2` 取重度；`<60%` 取中度；其余轻度。
话术口径：连错≥2 提示排查 / `<50%` 提示基础不牢 / `<60%` 提示刚跌破及格线 / 其余提示需巩固）：

**家长操作**：

| 操作 | 接口 | 效果 |
|------|------|------|
| 手动忽略 | `PUT /api/gaps/:id {status: 3}` | 该 KP **长期静默**，聚合时不再新建 active |
| 标记攻克 | 聚合自动 | `levels` 升到 Good/Mastered → `gaps.status=2` + `resolved_at` |

**清单查询**（待攻克与历史清单实现见 `DATABASE_DESIGN.md` `gaps` 表第八章 8.4 / 8.5 节；
排序 `severity DESC, error_count DESC, created_at ASC`）：

> **部分唯一索引** `uq_gaps_active_kid_kp` 保证同一孩子同一 KP 最多一条 active，
> 避免重复入清单。

### 4.7 AI 产出审核闭环

审核状态**内联到 `questions`**（0 = 未审核 / 1 = 已确认 / 2 = 已纠正；枚举见 `DATABASE_DESIGN.md` `questions` 表第四章），
用 `review_status` + `reviewed_by*` + `review_note` 承载。

**审核动作**（流程见 `DATABASE_DESIGN.md` `questions` 表第六章 6.2 节）：

```
家长追加审核：
  若意见非空 → review_note = appendReviewNote(...) 且 review_status = 2（corrected）
  若意见为空 → review_status = 1（confirmed）
  统一写 reviewed_by / reviewed_by_type=1(parent) / reviewed_at = now
  → 可选触发重新聚合（按 tasks）

孩子标对错：
  is_correct = 新值；is_correct_source = 3（kid）
  review_note 追加"孩子标：我做对了/做错了"
  reviewed_by_type = 2（kid）；reviewed_at = now
  → 按 tasks 触发重新聚合
```

**`review_note` 追加算法**（追加不覆盖，保留全部痕迹；实现见 `DATABASE_DESIGN.md` `questions` 表第六章 6.3 节；
格式示例：`本题选 B，不是 A。...（妈妈）审核`，多条以 `<br />` 分隔）：

**待审查询**（实现见 `DATABASE_DESIGN.md` `questions` 表第六章 6.4 节；条件：所属卷有效 + `review_status=0`）：

> **闭环门禁**：只有 `review_status=1`（已确认）的题目才进 4.5 节聚合。
> `review_status=0` 的 AI 产出**不污染** `levels` / `gaps`。

### 4.8 答案纠正重判

直接改 `questions` 并用 `reviewed_at` + `review_note` 留痕。

```
1. 家长 / 孩子纠正答案：
     correct_answer      = 新答案
     answer_source       = 2（parent）
     is_correct          = judge(新答案, student_answer, question_type)
     is_correct_source   = 2(parent) / 3(kid)
     review_status       = 2（corrected）
     reviewed_by / reviewed_by_type / reviewed_at / review_note 同步更新
2. 入队 tasks(kind=1, detail={"kid_id":X}) → 触发 levels / gaps **全量重算**
```

- **无需版本链**：无分数、无 `answer_key_version`，纠正只影响 `is_correct` 布尔，
  影响面由**全量重算**兜住（`levels` 是派生态，不是增量账本）。
- **历史痕迹不丢**：旧答案写进 `review_note`（追加式，见 4.7 节），满足审计诉求。

### 4.9 文科主观题方案

`questions.question_type` 含主观题型：`QuestionTypeSolve`（解答）、`QuestionTypeShortAnswer`（简答）、
`QuestionTypeWriting`（写作）、`QuestionTypeComprehensive`（综合）、`QuestionTypeProve`（证明）。

- **无分数、无难度**：主观题只产 `is_correct` 布尔，**不做自动评分**。
- **判定主体**：AI 给初判（`is_correct_source=1`），**家长 / 孩子终判**（`=2` / `=3`），
  主观题默认走家长确认（低风险客观题可由孩子标）。
- **无「能力项」建模**：主观题只挂 `question_kps` 的知识点，不设跨学科能力维度。

### 4.10 学情诊断报告

报告 = `levels`（水平面）+ `gaps`（薄弱面）+ `questions`（证据面）三表组合，**无独立报告表**。

| 报告维度 | 数据来源 |
|----------|----------|
| 学科知识树 | `levels` JOIN `knowledge_points`（按 `sort_order`） |
| 薄弱项排序 | `gaps WHERE status=1` ORDER BY `severity DESC, error_count DESC` |
| 诊断话术 | `gaps.reason_hint`（`BuildReasonHint` 预生成，见 4.6 节） |
| 最近错题证据 | `gaps.latest_question_id` → `questions`（应用层校验 `status=1`，否则显示"已删除"） |
| 待审量 | `questions WHERE review_status=0` 计数 |

**冷启动口径**（见 `DATABASE_DESIGN.md` `levels` 表第五章）：

| 条件 | UI 表现 |
|------|---------|
| `correct_count + wrong_count < 3` | 「未训练 / 样本不足」，不参与薄弱项排序 |
| `total >= 3` 但正确率 `< 60%` | 列入薄弱清单，附 `reason_hint` |
| `total >= 5` 且 `≥85%` 且近 3 题全对 | 标记「已掌握」，对应 `gaps.status=2` |

### 4.11 图文混排导出

题图命名采用**卷内路径 + 页内序号**（与 3.18 节布局一致）：

```
命名规则：
  试卷页图：assets/exams/{exam_id}/page_{index:03d}.webp
  题图    ：assets/exams/{exam_id}/questions/q_{question_no:03d}.webp
  导出文件名：{exams.title 或 subject_name}_{exam_date}.pdf / .md
```

- **无跨卷同名冲突**：卷内强绑定目录天然隔离，无需冲突加后缀逻辑。
- **题面导出**：Markdown + LaTeX（`questions.content`），题图按 `image_path` 内联。

### 4.12 试卷修正与 OCR 重切

**单事务替换题集**（流程见 `DATABASE_DESIGN.md` `questions` 表第六章 6.5 节）：

```
BEGIN
1. DELETE FROM questions WHERE exam_id = ?      -- 级联清 question_kps；gaps.latest_question_id 置 NULL
2. 从页图重新裁切 → 落盘新题图（覆盖 assets/exams/{id}/questions/q_*.webp）
3. INSERT questions(...) × N                    -- 新题集，review_status=0
4. INSERT question_kps(...) × M                 -- AI 重新打标
5. 入队 tasks(kind=1, detail={"kid_id":X})       -- 重新聚合 levels / gaps
COMMIT
```

- **冲突根因与解法**：重切时新旧题集 `question_no` 必撞 `UNIQUE(exam_id, question_no)`，
  故**先删后插**而非并存。
- **重切前提示**：「将覆盖旧题集与审核痕迹」。保留旧版需改 `UNIQUE` 为
  `UNIQUE(exam_id, question_no, status)`——**当前不做**（YAGNI）。
- **审核痕迹处理**：`review_note` 随题删除而消失；如需留痕应先导出。

### 4.13 通知中心

`messages` 承载给**人**看的通知（给 Worker 的事走 `tasks`，见 3.1 节三表边界）。

**收件人两类**（DB CHECK 强制）：家长（`audience=1`，`kid_id` 必 `NULL`，全家共享）
/ 孩子（`audience=2`，`kid_id` 必填）。

| `kind` | 受众 | 触发时机 |
|--------|------|----------|
| `MessageKindGradeAdvanced`（1） | 家长 | `tasks(kind=2)` 年级推进完成后，提示「如不符请修改」 |
| `MessageKindBackupDone`（2） / `MessageKindBackupFailed`（3） | 家长 | 备份任务（`tasks(kind=3)`）执行完 |
| `MessageKindAIReviewPending`（4） | 家长 | `questions.review_status=0` 积压达阈值时 |
| `MessageKindGrade7Reminder`（11） | 家长 | `tasks(kind=2)` 推进到初一（目标 `grade=7`）时，提示科目变多、可到孩子设置确认科目完整（链接 `/kids/<id>`） |
| `MessageKindGrade9Reminder`（9） | 家长 | `tasks(kind=2)` 推进到初三时，提示生物 / 地理应已结课、可到科目管理隐藏 |
| `MessageKindGrade10Reminder`（10） | 家长 | `tasks(kind=2)` 推进到高一（目标 `grade=10`）时，提示可到孩子设置设置文理科（链接 `/kids/<id>`） |
| `MessageKindOther`（99） | 家长 | 兜底 |

- **`kind=5~8` 预留未启用**：`MessageKindStorageCleaned` / `MessageKindGapResolved` / `MessageKindReviewDue` / `MessageKindGradeChanged`
  当前**无写入方**。新增通知类型时**必须同步落地写入方**（见 3.14 节）。`kind=11`（`MessageKindGrade7Reminder`）由 `tasks(kind=2)` 在目标年级为初一（`raw_target=7`）时写入；`kind=9`（`MessageKindGrade9Reminder`）在目标年级为初三时写入；`kind=10`（`MessageKindGrade10Reminder`）在目标年级为高一（`raw_target=10`）时写入。
- **已读语义**：家长通知「已读」= 全部孩子均已读不需要——家长通知是全家共享的单条，
  `is_read` 只表示家长本人是否读过；孩子通知则按孩子各自 `is_read` 独立计。
- **`link` 字段**：前端深链（如 `/kids/1`、`/gaps`），点击直达对应页面。

---

## 五、AI 深度介入方案

> **原则不变**：AI 产出**默认 `review_status=0`（未审核）**，不直接进业务闭环；
> **最终确认权在家长**（低风险客观题可由孩子标对错）；**无 AI 时核心功能可用**（离线降级）。

AI 产出**不落独立审核队列表**——审核状态内联到 `questions`（见 3.9 节 / 4.7 节），

| 功能 | AI 角色 | 落库位置 | 审核 | 孩子反馈 |
|------|---------|----------|------|----------|
| 试卷上传 | 分类归属建议（学科 / 年级 / 类型） | `exams.subject_id` / `grade` / `exam_type` | 家长确认后可改 | 无（系统内部） |
| OCR 切分 | 从页图裁切题目区域 | `questions.image_path` + `content` | `content_source=1`，家长可修正（`=2`） | 无 |
| 标准答案 | 答案提取 | `questions.correct_answer` | `answer_source=1`（AI）/ `=2`（家长修正） | 无（限家长改） |
| 知识点打标 | 题目↔知识点关联 | `question_kps` | `source=1`（AI）/ `=2`（家长调整） | 无 |
| 对错判定 | 客观题自动判对错 | `questions.is_correct` | `is_correct_source=1`（AI）/ `=2`（家长）/ `=3`（孩子） | **可标对错** |
| 摘要生成 | 卷面摘要 | `exams.summary` | `summary_source=2`（AI），可改 `=1`（手工） | 无 |
| 薄弱点话术 | 诊断话术生成 | `gaps.reason_hint` | 规则生成（`BuildReasonHint`），非 AI | 无 |
| 主观题评分 | **不做**（只给初判） | `questions.is_correct` 初判 | **家长终判**，无分数 | **可标对错** |
| 错题图片级诊断 | **已移除** | — | — | — |
| 试卷级失败归因 | **已移除** | — | — | — |
| 跨学科能力打标 | **已移除** | — | — | — |

> **孩子反馈落点**：`questions.review_note`（追加式，见 4.7 节）+
> `is_correct_source=3` / `reviewed_by_type=2`。
> **无 AI 时**：上传 / 列表 / 详情 / 录题 / 改对错 / 聚合 / 薄弱点清单全部可用，
> 仅「自动切题 / 自动抽答案 / 自动打知识点 / 自动判对错」缺失（见第七章离线可用）。

---

## 六、预置学科与知识点

### 6.1 学科字典（13 科）

学科是**独立字典表** `subjects`（见 3.7 节），首启由 `internal/seed/data/subjects.json`
落盘到 `<dataDir>/seed/subjects.json`，再经 `cursors` 幂等导入（见 8.5 节）。

`mask` 是 **5 bit 可见位**：低 3 位=学段（1 小学 / 2 初中 / 4 高中），高 2 位=文理（8 文 / 16 理）。
科目对某孩子可见 ⟺ 学段位 AND 文段位同时命中；**文理维仅 `grade ≥ 10` 生效**（`grade < 10` 时 track 视为 0）。

| `code` | `name` | `sort_order` | `mask` | 说明 |
|--------|--------|--------------|--------|------|
| `chinese` | 语文 | 10 | 31 | 全学段 + 文\|理（必选） |
| `math` | 数学 | 15 | 31 | 全学段 + 文\|理（必选） |
| `english` | 英语 | 20 | 31 | 全学段 + 文\|理 |
| `biology` | 生物 | 25 | 22 | 初高 + 理科 |
| `geography` | 地理 | 30 | 14 | 初高 + 文科 |
| `history` | 历史 | 35 | 14 | 初高 + 文科 |
| `politics` | 道法 | 40 | 31 | 全学段 + 文\|理 |
| `physics` | 物理 | 45 | 22 | 初高 + 理科 |
| `chemistry` | 化学 | 50 | 22 | 初高 + 理科 |
| `science` | 科学 | 55 | 25 | 小学 + 文\|理 |
| `art` | 美术 | 60 | 31 | 全学段 + 文\|理 |
| `music` | 音乐 | 65 | 31 | 全学段 + 文\|理 |
| `pe` | 体育 | 70 | 31 | 全学段 + 文\|理 |

- **`source=1`**（`SubjectSourceSystem`）为系统预置；家长可新增 `source=2`（`SubjectSourceUser`）自定义学科。
- **学科名改 `name` 不影响引用**——外键只认 `id`，`code` 供程序稳定引用。
- **必选科（`mandatory=1`）**：语文 / 数学，孩子在 `kid_subjects` 白名单中不可取消（见 3.7 与 `kid_subjects` 表设计）。
- **`kid_subjects` 集合**：孩子实际所学学科的个体集合；上传页 / 雷达图 / 试卷分布 / 归档统计一律以其为准，**无记录即空**（无 `mask` 回退，见 3.7 节）。

### 6.2 年级与考试类型（结构化列，非标签）

| 维度 | 载体 | 取值 |
|------|------|------|
| 年级 | `exams.grade` / `kids.grade` | `INTEGER` **1~12**，`CHECK (grade BETWEEN 1 AND 12)`；映射初一=7 / 初三=9 |
| 考试类型 | `exams.exam_type` | `INTEGER` 枚举：1 周测 / 2 月考 / 3 期中 / 4 期末 / 5 模考 / 6 专项 / 99 其他（见 3.5 节） |

> **无权重映射**：`exam_type` 仅作筛选维度，**无难度维度即无权重需求**（见 4.5 节）。

### 6.3 知识点字典

知识点是**独立字典表** `knowledge_points`（见 3.8 节），两级固定（学科 → 知识点），
`UNIQUE(subject_id, name)`，**无 `code`、无别名、无 `usage_count`、无关系边**。

- **无关系边表** ⇒ 无「前置 / 后继」关系 ⇒ **无掌握度传播算法**。
- **无难度维度** ⇒ `questions` 不存难度（见 3.9 节）。
- 知识点内容**教研侧维护**（后续版本），由家长按需新增（`source=2`）。

## 七、非功能性需求

| 指标 | 要求 |
|------|------|
| 响应速度 | 结构化列筛选 < 100ms（走 B-tree 索引，见 4.4 节）；AI 分析 < 10s；OCR < 3s/页 |
| 数据安全 | 本地 SQLite（modernc.org/sqlite）+ 文件系统 |
| 离线可用 | 无 AI/OCR 时核心功能可用（上传 / 录题 / 改对错 / 聚合 / 薄弱清单） |
| 跨平台 | Windows / macOS / Linux（纯 Go 无 CGO，单二进制） |
| 可导出性 | 报告 .md（含图片包），题目 CSV |
| 备份 | `VACUUM INTO` 在线备份 + 分级快照（分级与份数见 `DATABASE_DESIGN.md` 第二章 2.1 节） |
| 数据量 | 单孩子 3 年 ~50MB DB + ~5GB 文件 |
| 每日维护 SLA | < 5 分钟；`aggregate_gaps` 按 kid 全量重算（数据量极小，见 4.5 节） |
| 聚合幂等 | `levels` / `gaps` **必须全量重算**，禁止 `+= 1` 增量（见 4.5 节） |
| PIN 哈希 | 家长 / 孩子 PIN 必须用慢哈希 bcrypt / argon2id / scrypt（cost 足够），禁止明文 / MD5 / SHA-1 |
| access_code 熵 | 随机熵 ≥ 40 bit、字符集排除易混字符、碰撞重试至唯一（见 3.4 节） |
| 登录限流 | 家长 PIN / 孩子 PIN 连续失败限流（如 5 次锁 15 分钟），本地亦防暴力 |
| 枚举存储 | 存储值一律**英文 / INT**，中文只在 DTO 层映射（AGENTS.md 规范） |

---

## 八、运维设计

| 表 | 职责 | 不承担 |
|----|------|--------|
| `tasks` | **事件队列**——异步工作的排队与重试 | 不做断点续跑 |
| `cursors` | **断点 / 一次性标记**——seed 只导一次、大任务分批 | 不做事件排队 |
| `metrics` | **日聚合打点**——体检看板数据源 | 不做业务事务回滚 |

### 8.1 Schema 迁移

流程：**暂停写入 → `VACUUM INTO` 备份 → 事务迁移（DDL 事务包裹）→ 校验 → 更新版本**。

- **迁移判重**：`migrations` 表按版本号判重，已应用的版本跳过。
- **版本记录**：迁移后写 `migrations(version, display_tag)`（含 `version` + `display_tag`）。
- **失败回滚**：迁移在事务内，失败整体回滚，不留半迁移状态。
- **版本读取的 SQL 兼容防御**（空表不能返空集）：实现见 `DATABASE_DESIGN.md` 第二章 2.2 节
  （外层包子查询 `COALESCE((SELECT …), 'unknown')`，恒定输出单行）。

> **COALESCE 陷阱**（AGENTS.md 已固化）：`COALESCE(col, 'x')` 对**空结果集**无效，
> 必须写成外层包子查询 `COALESCE((SELECT ...), 'x')`。

### 8.2 备份策略

- **在线备份**：`VACUUM INTO 'backup/paperly-{YYYYMMDD}.db'`——不停机。
- **保留策略**：`backup/` 分级保留 **daily 7 份 / weekly 4 份 / monthly 12 份**，超出按日期淘汰最旧。
- **备份任务化**：`tasks(kind=3, priority=80)`，Worker 异步执行，
  结果打点 `metrics` 的 `backup_success` / `backup_fail`，并写 `messages`（4.13 节）。
- **资产文件不进备份**：备份只覆盖 DB。`assets/` 靠「卷内强绑定 + 删卷即删目录」
  管理（见 8.4 节），恢复时以 DB 记录为准重新对齐文件。
- **恢复演练**：删除一份快照 → `VACUUM INTO` 还原 → 校验 16 表行数与 `migrations.display_tag`。

### 8.3 AI / OCR 降级策略

| 能力 | Provider 未配置时 | 降级路径 |
|------|------------------|----------|
| OCR 切题 | `OCRProvider.IsAvailable() = false` | 跳过切分，走「空卷 + 手工录题」 |
| 答案抽取 | `LLMProvider.IsAvailable() = false` | `correct_answer` 留空，家长 / 孩子手工填 |
| 知识点打标 | 同上 | `question_kps` 无行 ⇒ 该题不进聚合（`knowledge_points` 需人工关联） |
| 客观题判对错 | 同上 | `is_correct` 由人工填；`is_correct_source=2/3` |
| 薄弱点话术 | **不依赖 AI** | `BuildReasonHint` 规则生成（见 4.6 节） |

> **核心功能零依赖**：上传 / 列表 / 详情 / 录题 / 改对错 / 聚合 / 薄弱清单 /
> 通知**全部不依赖 AI / OCR**——无 Provider 时可用（见第七章离线可用）。

### 8.4 资产文件与硬删

**分布**（见 `DATABASE_DESIGN.md` 第二章 2.1 节；含 `assets/tmp/` 暂存区与 `backup/` 分级快照）。

**硬删两阶段**（流程见 `DATABASE_DESIGN.md` `exams` 表第六章 6.6 节）——删目录是慢 I/O，不能进事务：

```
阶段一（事务内，秒级响应）：
  BEGIN
  INSERT INTO tasks(kind=4, priority=80, status=0,
                    detail=json_object('dir','assets/exams/' || ?));  -- cleanup_storage
  DELETE FROM exams WHERE id = ?;   -- 外键 CASCADE 清 pages / questions / question_kps
  COMMIT                            -- API 立即返回「删除成功」

阶段二（Worker 异步）：
  os.RemoveAll(payload.Dir) → 成功 tasks.status=2；失败按 retry_count 重试
```

- **路径穿越防护**：`detail.dir` 由 Go 层用
  `filepath.Join(baseDir, "exams", strconv.Itoa(examID))` 生成，并**校验前缀**为 `assets/exams/`。
- **回收站**：软删（`status=2`）只改状态、文件留在磁盘（见 `DATABASE_DESIGN.md` `exams` 表第六章 6.7 节）；
  `GET /api/exams/trash` 不带 `EXISTS(pages)` 兜底——孤儿 exam 是 `status=1`，不在此列。
- **不做 30 天自动硬删**：避免家长想恢复时已被清。

### 8.5 Box 首次启动

```
1. seed.EnsureSeed(dataDir)        -- embed.FS → <dataDir>/seed/（家长可编辑的产物）
2. seed.LoadSubjects(dataDir)      -- 读 subjects.json
3. cursors 判重：SELECT last_status FROM cursors WHERE task_name='seed_subjects'
   若无记录 → 导入 subjects 表 → INSERT cursors(task_name='seed_subjects', last_status=2, last_run_at=now)
   若 last_status=2 → 跳过（**只导一次**）
4. 无记录时引导创建首个家长账号 + 引导添加孩子档案
```

- **seed 导入幂等靠 `cursors` 一次性标记**，不靠「先查后插」判空。
- **`knowledge_points` 目前无 seed 消费者**——`seed_knowledge_points` 仅为预留游标名（见 3.13 节）。

### 8.6 数据体检

- **指标源**：`metrics` 日聚合（打点口径见 3.16 节），体检读「近 7 天合计」：
  `SELECT COALESCE(SUM(count),0) FROM metrics WHERE metric_name=? AND metric_date >= date('now','-6 days')`。
- **任务队列**：待办 / 失败数（`tasks WHERE status=0` / `status=3`）。
- **游标状态**：`SELECT task_name, last_id, last_run_at, last_status FROM cursors ORDER BY task_name`。
- **保留策略**：`metrics` 留 **90 天**（每日维护顺带清理）；`tasks` 清 30 天前的
  `done/failed/skipped`；`cursors` 不清理。
- **孤儿 exam 兜底**：列表加 `EXISTS (SELECT 1 FROM pages WHERE exam_id=e.id)`（见 4.1 节）；
  未来加 `cleanup_orphan_exams` 任务扫崩溃残留。

### 8.7 每日维护（`DailyMaintenance`）

- **不依赖游标**——家庭场景数据量小，按 kid 全量重算即可（与 `aggregate_gaps` 同口径，
  失败靠 `tasks.retry_count` 重试）。
- **执行内容**：纯内存指标重算 + `metrics` 90 天清理 + `tasks` 30 天清理。
- **`updated_at` 语义**：**仅对统计值实际变化的 `levels` 行**在事务末尾刷新 `updated_at`
  ——**不做全表 touch**，保留「该行确实变过」的审计语义。
- **打点**：成功 `maintenance_success` / 失败 `maintenance_fail`。

### 8.8 异步任务队列（`tasks`）

`tasks` 做事件队列（消费流程见 `DATABASE_DESIGN.md` `tasks` 表第七章）。

**`kind` 与 `detail` 载荷、消费循环、幂等与优先级口径**见 `DATABASE_DESIGN.md` `tasks` 表第四章（枚举）、
第七章 7.1–7.2 节（入队与消费）、第六章（入队策略）：`detail` 为 JSON（Payload-Only，不建物理外键），
Handler 消费时自行查目标、查不到标 `status=4(skipped)`；允许重复入队（全量重算幂等）；
`priority` 0~100（默认 50），越大越先跑；组合索引 `(status, priority DESC, created_at ASC)` 服务取下一个待办；
失败按 `retry_count` 重试（`<3` 重置 pending，`≥3` 保持 failed）。

### 8.9 异步执行模型（全貌）

进程启动两个**互不相同的常驻 goroutine**（`cmd/server/main.go`）：

| goroutine | 职责 | 循环 | 写入 |
|-----------|------|------|------|
| **Daily maintenance** | 每日维护：采集体量快照、清理过期 `tasks`/`metrics`、9/1 入队年级推进 | 每日一次，靠 `cursors` 判重 | `metrics`、`tasks` |
| **Task worker** | 消费 `tasks` 队列：Claim 队头 → 按 kind 分派 → 写终态 | 空队列时每 30s 轮询，立即接下一个 | `tasks`、受影响的业务表 |

**两者协作**：maintenance 只负责「入队」（如 9/1 年级推进 enqueue + 备份任务通常由界面/接口触发），worker 只负责「消费执行」。同一 `tasks` 表，入口不同。

**串行消费模型**：task worker 是**单个常驻 goroutine**，一次只消费一个任务（避免同一任务被两次 Claim；写池单连接是第二重保险）。Box 单机任务量极低，串行足够，**不拆分多 worker**（拆分只会引入"同一任务被多 worker 抢"的协调复杂度，属过度设计）。

**同步 / 异步划分**（哪些经 worker，哪些不经）：

| 操作 | 执行方式 | 说明 |
|------|----------|------|
| 试卷上传 | **同步即时** | `exams`/`pages` 直接落库，不排队 |
| 试卷删除 / 硬删 | **同步即时**（硬删时入队清理文件） | 软删只改 `status`；硬删事务内删 DB 行 + **异步入队 `cleanup_storage`**（`os.RemoveAll` 是慢 I/O，不进事务） |
| 页重排（删 / 加 / 排页） | **异步入队**（kind=5） | 涉及临时文件与批量插页，异步更稳 |
| 备份 | **异步入队**（kind=3） | 界面/接口触发 |
| 每日统计 | **独立 goroutine 直接执行** | maintenance，不经 worker（仅年级推进经队列） |
| 年级推进 | maintenance **入队**（kind=2）→ worker 消费 | 9/1 一次性；幂等靠 `kids.grade == enrolled_grade` |

**结果日志**：worker 对每种终态都写结构化日志（`internal/worker/worker.go`）：

| 结果 | 级别 | 消息 | 字段 |
|------|------|------|------|
| 启动 | Info | `background task consumer goroutine started` | `poll_interval` |
| 领取失败 | Error | `failed to claim task` | `err` |
| 完成 | Info | `task finished` | `task_id` + `kind` |
| 跳过 | Info | `task skipped` | `task_id` + `kind` + `reason` |
| 执行失败 | Error | `task execution failed` | `task_id` + `kind` + `err` |
| 终态写失败 | Error | `failed to mark task finished/skipped/failed` | `task_id` + `kind` + `err` |

> **字段覆盖**：完成/跳过/失败日志均含 `task_id` + `kind`（可精确追溯是哪个任务、哪类）；不含
> `priority`/`detail`/`retry_count`（排查时按 `task_id` 查 `tasks` 表即可补齐）。领取失败时无任务可记，
> 仅 `err`。

---

## 九、成本模型

### 9.1 归属声明

> 下表为 **Box 用户接入第三方 API 的参考月支出（用户自担）**。
> Paperly 本身不承担此成本。

### 9.2 真实区间单价（附出处）

| 项 | 单价（区间） | 出处 |
|----|--------------|------|
| OCR（阿里云通用） | ¥0.055~0.0825/次 | 阿里云官方计费页（2026-01 取价） |
| AI（DeepSeek） | ¥0.005/次（5k tokens） | DeepSeek 官方价 |
| AI（通义千问） | ¥0.02/次 | 阿里云百炼官方价 |
| AI（GPT-4o） | ¥0.15/次 | OpenAI 官方价 |

### 9.3 月成本区间

| 项 | 低 | 高 |
|----|----|----|
| OCR（50 页） | ¥2.75 | ¥4.13 |
| AI（25 次） | ¥0.13 | ¥3.75 |
| **合计（用户自担）** | **¥2.88** | **¥7.88** |

---

## 十、开发优先级

### 10.1 阶段划分

| 阶段 | 目标 | 状态 |
|------|------|------|
| **P0-地基** | PoC + 16 表 schema + 迁移 + 备份 + 写池串行 | ✅ 已完成（ADR 0001） |
| **P1-闭环** | 上传 → 切题 → 录答案 → 校对 → 聚合 `levels` → 派生 `gaps` | 🔶 进行中（地基 7/16 表已落地） |
| **P2-家庭** | 家长/孩子双入口 + `access_code` 免登录 + `messages` 通知 | ⬜ 未开始 |
| **P3-增强** | OCR / AI 适配器 + 导出 + 体检 + 恢复演练 | ⬜ 未开始 |

### 10.2 任务表

| 优先级 | 功能 | 建表 | 点亮 | 状态 |
|--------|------|------|------|------|
| **P0** | SQLite PoC（9 项，ADR 0001） | — | — | ✅ |
| **P0** | 16 表 schema（`migrations/0001_init.sql`） | ✅ | — | ✅ |
| **P0** | 读写池分离 + 写池串行（ADR 0001） | — | — | ✅ |
| **P0** | `subjects` 字典 + seed 导入（`cursors` 判重） | ✅ | ✅ | ✅ |
| **P0** | `exams` + `pages` 上传 / 列表 / 详情 / 编辑 | ✅ | ✅ | ✅ |
| **P0** | `kids` 档案 CRUD + `access_code` 生成 | ✅ | ✅ | ✅ |
| **P0** | `metrics` 打点 + 体检查询 | ✅ | ✅ | ✅ |
| **P0** | 备份（`VACUUM INTO`） | — | — | ✅ |
| **P1** | `questions` 单表题库（题面 + 作答 + 对错 + 审核内联） | ✅ | ⬜ | 仓储待建 |
| **P1** | `question_kps` 题目↔知识点关联 | ✅ | ⬜ | 仓储待建 |
| **P1** | 逐题校对 UI（题面 + 题图 + AI 答案 + AI 判对错同屏） | — | ⬜ | |
| **P1** | `levels` 聚合 Worker（`tasks(kind=1)` + 全量重算） | ✅ | ⬜ | 仓储待建 |
| **P1** | `gaps` 派生（`CalculateSeverity` + `BuildReasonHint`） | ✅ | ⬜ | 仓储待建 |
| **P1** | 答案纠正重判（全量重算兜影响面） | ✅ | ⬜ | |
| **P1** | `knowledge_points` 知识点字典 + 家长 CRUD | ✅ | ⬜ | 已实现 |
| **P1** | `tasks` 事件队列 Worker（kind 1/2/3/4） | ✅ | ⬜ | 已实现（kind=1 聚合占位跳过） |
| **P2** | 家长 `users` 登录（用户名 + PIN + 限流） | ✅ | ⬜ | 已实现 |
| **P2** | 孩子免登录入口 `/kid/{access_code}`（+ 可选 PIN） | ✅ | ⬜ | 已实现 |
| **P2** | `messages` 通知中心（家长共享 / 孩子独立） | ✅ | ⬜ | 已实现 |
| **P2** | 硬删两阶段（`tasks(kind=4)` + `RemoveAll`） | ✅ | ⬜ | 已实现 |
| **P2** | 回收站（软删列表 + 恢复） | ✅ | ⬜ | 已实现 |
| **P3** | OCR 适配器 + 规则切分 | — | ⬜ | |
| **P3** | LLM 适配器（答案抽取 + 知识点打标 + 判对错） | — | ⬜ | |
| **P3** | `review_note` 追加式审核留痕 | ✅ | ⬜ | |
| **P3** | 试卷修正与 OCR 重切（先删后插单事务） | ✅ | ⬜ | |
| **P3** | 图文混排导出（Markdown + 题图内联） | — | ⬜ | |
| **P3** | 学情诊断报告（`levels` + `gaps` 组合视图） | — | ⬜ | |
| **P3** | 每日维护 + 恢复演练 | — | ⬜ | 已实现（每日维护；恢复演练无落地） |

> **`questions` / `question_kps` / `levels` / `gaps` 的 domain 与
> repository 尚未实现**——表已建但无读写方，是 P1 的主要工作量。

---

## 十一、家庭内用户与角色

> **实例即家庭、单实例单家庭**。两类主体：家长用 `users` 账号登录管理全家；
> 孩子是数据主体兼**一等用户**，凭 `kids.access_code` 免登录进入，
> **不加 `user_id`、不建 `user_kids` 中转表**——家庭版里多家长天然可见全家，
> 无需多对多授权关联。多家长各建一个 `users` 账号即可。Cloud 版如做多租户再考虑关联表（Cloud 暂缓）。

### 11.1 两类账号

| 主体 | 表 | 登录 | 角色枚举 |
|------|----|------|----------|
| 家长 | `users` | 用户名 + PIN（`pin_hash` 慢哈希） | 无 `role` 列——家长身份由 `users` 行本身承载（不设 child、不提供 teacher） |
| 孩子 | `kids` | `access_code` 免登录（+ 可选 PIN） | 无 `role` 列——孩子身份由 `kids` 行本身承载 |

- **`users` 与 `kids` 是平行表**：`users.id` 不出现在 `kids` 中（无归属上级），
  孩子是独立主体而非某个家长的子账号。
- **`reviewed_by_type` / `reviewed_by` 指向哪类账号**（映射见 `DATABASE_DESIGN.md` `questions` 表第四章 `reviewed_by_type`）：
  `1`=parent → `users.id` / `2`=kid → `kids.id`（孩子无 `users.id`）。

### 11.2 访问方式与权限

| 身份 | 进入方式 | 权限 |
|------|----------|------|
| 孩子 | `/kid/{access_code}`（+ 可选 PIN） | 看自己的 `levels` / `gaps` / 题目；上传试卷（可配）；标题目对错（`is_correct_source=3`）；追加 `review_note` 反馈。**不可**改标准答案、删数据、看其他孩子、管理账号，孩子不可改 review_status；孩子软删、硬删归家长。 |
| 家长 | 用户名 + PIN 登录 | 可见全部孩子；审核 AI 产出、改答案、删数据、管理账号；管理 `kids.access_code`（查看/复制/重置）、重置 PIN；管理学科与知识点 |
| 访客 | 无入口 | — |

> **原则**：孩子可看、可反馈、可提交；家长最终审核、管理、可改可删。
> 孩子 Dashboard：我的薄弱点清单 / 我的掌握度 / 我的错题 / 上传入口 / 反馈入口。
> 家长 Dashboard：多孩子切换 / 待审题目 / 报告与趋势 / 账号与权限 / 学科知识点管理

---

## 十二、边界情况与风险

| 风险 | 应对 |
|------|------|
| SQLite PoC 不通过 | 备选 PostgreSQL（见第十四章） |
| 行锁不可用 | 停机备份 + 全量重算停机 |
| OCR 准确率低 | 人工校对（`questions.content_source` 修正） |
| AI 成本失控 | 本地 AI + `metrics` 打点 + 限额 |
| 单文件损坏 | `VACUUM INTO` 分级快照（daily 7 / weekly 4 / monthly 12） |
| 迁移失败 | 停机 + 事务回滚 + 丢量告知 |
| 误删试卷 | 三态（`status` active/deleted）+ 回收站 |
| 删卷文件残留 | 两阶段硬删：`tasks(kind=4)` 异步 `RemoveAll` + 重试（见 8.4 节） |
| 上传崩溃留孤儿 | `EXISTS(pages)` 过滤 + `cleanup_orphan_exams` 任务（见 4.1 节） |
| 增量聚合导致翻倍 | **硬要求全量重算**，禁止 `+= 1`（见 4.5 节 / 第七章） |
| 重切撞 `question_no` UNIQUE | 先删后插单事务（见 4.12 节） |
| 答案纠正后 `levels` 陈旧 | 纠正即入队 `tasks(kind=1)` 全量重算（见 4.8 节） |
| 孤儿 `tasks`（kid 已删） | Handler 内查 `kids` 不存在 → `status=4(skipped)`（见 8.8 节） |
| 路径穿越 | `detail.dir` 前缀校验为 `assets/exams/`（见 8.4 节） |
| 每日维护超时 | 数据量小，`retry_count` 重试；不依赖游标（见 8.7 节） |
| 提交并发写冲突 | 单写池（`SetMaxOpenConns(1)`）+ `busy_timeout` + 写池串行（ADR 0001） |
| `metrics` 表膨胀 | 每日维护清 90 天前（见 8.6 节） |

---

## 十三、附录

### 13.1 术语表

| 术语 | 说明 |
|------|------|
| **家庭边界** | 本版确立：系统仅服务单个家庭内部；家长=管理员与 AI 结果审核者，孩子=数据主体，AI=助手。不提供教师端 / 班级管理 / 家校沟通 / 学校账号（见 1.3 节） |
| **家庭实例** | 实例即家庭、单实例单家庭——不是多家庭平台，也不是几家搭伙（见 1.3 节） |
| **掌握度水平（level）** | `levels.level` 四档枚举：`Unknown` 未训练（total<3）/ `Weak` 薄弱（正确率<60% 或近 3 题错≥2）/ `Good` 良好 / `Mastered` 已掌握（total≥5 且 ≥85% 且近 3 题全对）。**INT 枚举，无连续分** |
| **CalculateLevel** | 由 `correct_count` / `wrong_count` / `recent_wrong_count` 判定 level 的**纯函数**（见 3.11 / 4.5 节） |
| **全量重算** | `levels` / `gaps` 的唯一聚合方式：从 `questions` 重数后 UPSERT。**禁止增量累加**（会翻倍） |
| **薄弱点（gap）** | `gaps` 行：由 `levels.level=Weak` 派生，含 `severity` / `error_count` / `reason_hint` / `latest_question_id`；`status` 三态 active/resolved/ignored |
| **派生而非重聚合** | `gaps` 阶段直接取 `levels.wrong_count` 作 `error_count`，**不重数题**（避免口径漂移） |
| **家长忽略** | `gaps.status=3(ignored)`——聚合时跳过该 KP，不再新建 active，实现「长期静默」 |
| **已攻克** | `levels` 升到 `Good`/`Mastered` → `gaps.status=2(resolved)` + `resolved_at` |
| **审核闭环** | AI 产出默认 `questions.review_status=0(pending)`，**不进聚合**；家长/孩子确认（`=1`）或纠正（`=2`）后才进闭环 |
| **review_note** | `questions.review_note` 追加式审核留痕（`<br />` 分隔，保留全部历史，不覆盖） |
| **卷内强绑定** | 一份试卷的全部资产落在 `assets/exams/{exam_id}/`，删卷即删目录；**放弃跨卷 sha256 去重** |
| **上传者 vs 归属** | 两个独立事实：`exams.uploaded_by`（1=parent/2=kid，审计只读）/ `exams.kid_id`（归谁，业务可改） |
| **Payload-Only** | `tasks.detail` 是 JSON、**不建物理外键**；Handler 自行查目标，查不到标 `status=4(skipped)` |
| **一次性标记** | `cursors` 用 `last_status=2` 表示「已完成，不再执行」——seed 导入只跑一次 |
| **断点续跑** | `cursors.last_id` 主键游标（大任务分批，`id > last_id`）；当前无消费者，为未来预留 |
| **学生访问码（access_code）** | 创建孩子档案时生成的固定随机码，免登录入口 `/kid/{code}`，生成后不变，家长可重置（旧 URL 失效）；熵 ≥ 40 bit（见 3.4 节 / 第七章） |
| **家长账号（users）** | 家庭管理员账号，`role` 仅 parent/guardian（不设 child/teacher）；用户名 + PIN 登录，可见全家、可审核 AI、改答案、删数据、管理账号与孩子访问码 |
| **`reviewed_by_type`** | 标记 `reviewed_by` 指向哪类账号——`1`=parent → `users.id` / `2`=kid → `kids.id`（孩子无 `users.id`） |
| **孩子反馈** | 孩子作为一等用户对 AI 产出的反馈：标对错（`is_correct_source=3`）+ 追加 `review_note`；**不替代家长的最终确认权** |
| **`display_tag`** | `migrations` 的可读版本标签；读取用 `COALESCE((SELECT …), 'unknown')` 外层包子查询防空表返空集 |
| **单实例单家庭原则** | 本版顶层原则：实例即家庭、单实例单家庭。所有设计决策以单家庭场景为前提，不预留多租户 / SaaS / 跨家庭 / 多设备同步。遇「要不要为多租户预留」的问题，答案一律为**不预留**（见 1.3 节） |
---

### 13.2 品牌资产

| 项 | 内容 |
|----|------|
| 英文名 | Paperly |
| Slogan | Scan. Score. Succeed. |
| Box 口号 | Your data. Your server. Your rules. |

---

## 十四、Cloud 版差异标注（已排除）

> **本节已排除。**
>
> Cloud / SaaS / 多租户**不在当前版本的产品范围内**。下表仅为「若未来做 SaaS，与本版的差异点」，
> 作为**未来独立项目的输入**，**不约束当前任何设计决策**。
>
> 当前版本遇到「要不要为多租户预留」的问题，答案一律是**不预留**（见 1.3 节单实例单家庭原则）。

| 维度 | Box（本版） | Cloud（未来独立项目） |
|------|-------------|----------------------|
| 多租户 | ❌ | ✅ |
| 数据库 | SQLite（modernc.org/sqlite） | PostgreSQL（暂定） |
| 合规 | 用户自管 | 未成年人合规 |
| 审计 | 精简版 | 含 corrected_by/reviewed_by |
| 难度校准 | AI 预估 + 人工反馈 | 群体反算 |
| 并发 | 行锁/版本号 | 连接池 + 分布式锁 |
| 计费 | 一次性 | 月付 |

---

**文档结束**

> Paperly Box — Scan. Score. Succeed.
> Your data. Your server. Your rules.
