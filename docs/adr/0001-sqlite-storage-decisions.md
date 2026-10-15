# ADR 0001：SQLite 存储能力验证与存储决策

- 状态：已接受
- 决策者：人类（项目所有者）+ AI 执行验证
- 日期：2026-09-27
- 环境：Go 1.26.8 / modernc.org/sqlite v1.59.0 / Linux amd64 / tmpfs

## 背景

`REQUIREMENTS.md 第 2 章` 将 SQLite PoC 设为编码前置关卡。本 ADR 记录验证项的
执行结果，并给出存储决策，作为开始建表的前提。

全部 PoC 源码位于 `poc/`，每项独立可运行（`go run ./poc/0X_xxx.go`），输出格式统一。
PoC 8 支持 `--mb=` 缩小规模。

## 结论

| # | 项目 | 结果 | 关键数值/说明 |
|---|------|------|---------------|
| 1 | SQL 方言 | ✅ | `CREATE TABLE` / `INTEGER PRIMARY KEY AUTOINCREMENT` / `TEXT` / `DATETIME DEFAULT CURRENT_TIMESTAMP` 全部可用 |
| 2 | 自增主键 | ✅ | 连续插入 id=1/2/3；删除 id=2 后新插入 id=4（不复用） |
| 3 | 复合唯一约束 | ✅ | 复合唯一冲突返回 `UNIQUE constraint failed` |
| 4 | 索引 | ✅ | 普通/复合/部分索引均被 `EXPLAIN QUERY PLAN` 识别为 `USING INDEX`，无 `SCAN TABLE` |
| 5 | JSON 函数 | ✅ | `json_each` 展开 3 行；`json_array_length`=3；数组元素取用 `$[0]` |
| 6 | 单文件并发 | ✅ | 默认池并发写 `SQLITE_BUSY`（对照）；写池 1 连接 + `busy_timeout(5000)` + `_txlock=immediate` → **0 错误**；读池 10 → **0 错误** |
| 7 | 备份 | ✅ | `VACUUM INTO` 在线备份，源库不停机；1000 行一致，`integrity_check=ok` |
| 8 | 单文件上限 | ✅ | 写入 **1.01GB**（16384 行 × 64KB）无异常，耗时 3s |
| 9 | 乐观版本号 | ✅ | 同版本并发更新：success=1 / conflict=1 / busy=0；冲突以 `rowsAffected=0` 判定 |

**总评：9 项全部通过。**

## 关键决策

### 决策 1：连接池参数

- **写：单连接串行。** `SetMaxOpenConns(1)` + `SetMaxIdleConns(1)`，DSN 追加
  `_pragma=busy_timeout(5000)&_txlock=immediate`。
- **读：独立连接池。** `SetMaxOpenConns(10)`（WAL 模式下读不阻塞写）。
- 公共 DSN 基础：`_pragma=journal_mode(WAL)`。
- 依据：PoC 6 场景对比——默认池并发写 `SQLITE_BUSY` 率高；单写连接 +
  `busy_timeout` + `immediate` 事务锁下 **0 错误**。
- 与既有约定一致：`AGENTS.md` 的「写串行化走写池」（单连接 + `_txlock=immediate` + `busy_timeout`，为应用层单连接池机制，与驱动无关）。

### 决策 2：备份策略采用 `VACUUM INTO`

- 方案：在线执行 `VACUUM INTO '<backup_path>'`，生成一致性快照，**无需停机**。
- 依据：PoC 7 验证数据一致、`integrity_check=ok`、源库备份期间可继续服务。
- 替代语义：备份前 `PRAGMA wal_checkpoint(TRUNCATE)` 收敛 WAL；应用层维护一个
  「备份互斥 + 写池暂停水位」机制，再执行 `VACUUM INTO`。

## 遗留问题

1. **`_txlock=immediate` 与 `_pragma=busy_timeout` 是驱动级 DSN 参数**：需在
   `internal/repository` 中固化为读写双池，并在文档中记录，避免各处 `sql.Open`
   参数漂移。
2. **PoC 规模边界**：PoC 数据为合成数据；真实分布（行数、字段组合）可能不同，
   上线前宜用真实样本复测。

## 关联

- `REQUIREMENTS.md 第 2.1 节` PoC 验证项、`第 2.2 节` 结论分支、`第 8.2 节` 备份
- `poc/README.md`：运行方式与输出约定
- `AGENTS.md`：SQLite 使用规约（读写池 / 写池串行）
