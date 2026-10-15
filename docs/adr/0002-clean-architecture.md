# ADR 0002: 轻量 Clean Architecture + DTO 分离

- 状态：已接受
- 日期：2026-09-27
- 决策者：人类（项目所有者）裁定方向，AI 执行

## 背景

Day 4 引入分层架构时，需确定「Clean 到什么程度」以及「API 契约如何与领域模型分离」。
Day 4 初版实现为经典三层（`api → service → repository`，上层依赖下层具体实现），
后经评审调整为轻量 Clean；同时发现 `domain.Kid` 直接携带 `json` tag 参与
序列化，存在敏感字段暴露路径与契约分化问题。

## 决策

### 1. 轻量 Clean（不做全套）

采用「依赖倒置 + domain 纯净」，拒绝全套 Clean Architecture：

- **做**：
  - `domain` 层不 import 任何外部库（无 `sql`/`echo`/`zap`）
  - 接口定义在 `domain`，实现在 `repository`（依赖倒置）
  - `cmd/server/main.go` 负责依赖注入
  - 跨层共享的错误/枚举/常量放 `domain`
- **不做**：
  - UseCase 类（每操作一类）
  - Presenter / ViewModel 层
  - Mapper 库（手写转换）
  - Event Bus / CQRS
  - 每层独立 module

### 2. DTO 分离（强制）

**领域模型不做 JSON 序列化**——`domain.Kid` 等实体**不带 `json` tag**。

**原因**（非教条）：

- **安全**：`domain.Kid.PinHash` 若带 `json` tag 会被 `json.Marshal` 直接吐给
  前端，泄漏 bcrypt 哈希。PIN 的哈希在离线爆破下可破。
- **契约分化**：同一实体在不同视角需暴露不同字段——
  - `POST /api/kids` 响应：`access_code` **仅此一次**返回
  - `GET /api/kids` 列表：不含 `access_code`、不含 `pin_hash`
  - 孩子视角 `GET /api/me`（kid 受众）：不含 `access_code`（防分享）、不含 `pin_hash`
  - 若用同一 `domain.Kid` 序列化，必然「要么暴露过度、要么暴露不足」。

**结构**：

```
internal/
├── domain/kid.go              # 无 json tag，无外部 import
├── service/kid_service.go     # 返回 domain.Kid / 纯 Go 出参
└── api/
    ├── dto/kid.go               # 带 tag，按视角分（Created / Detail / Summary）
    └── kid_handler.go           # domain → DTO 转换
```

**三视角契约**：

| 视角 | 类型 | access_code | pin_hash |
|------|------|-------------|----------|
| 创建响应 | `dto.KidCreated` | ✅ 仅此一次 | ❌ 不存在 |
| 详情 | `dto.KidDetail` | ❌ 不存在 | ❌ 不存在 |
| 列表 | `dto.KidSummary` | ❌ 不存在 | ❌ 不存在 |

### 3. Cloud 版边界（附带）

- 本架构**不为 Cloud 版做超前设计**（不加 `TenantID`、不做方言抽象、不做分布式预留）
- 若未来 Cloud 版验证有需求：复用 `service`/`domain`/算法层（~80%），重写 `repository` 层（~20%），新增合规/认证/计费
- 轻量 Clean 的目的**是为 Box 版本身的可测性**，不是「为 Cloud 铺路」

## 后果

- `pin_hash` 在序列化层面**不可能**通过 API 泄漏（DTO 中无此字段）
- 未来新增视角（如孩子端）只需加 DTO，不需要撕扯 domain
- 测试可断言字符串级契约（`NotContains "pin_hash"`），已落地于 `kid_handler_test.go`
- 代价：api 层新增一层手工转换（当前约 40 行，随实体增长线性增加，可接受）

## 验证

```bash
grep -r 'json:' internal/domain/          # 应 0 命中
grep -rn 'pin_hash' internal/api/dto/     # 应 0 命中
curl -X POST localhost:8080/api/kids -d '{"name":"x","pin":"1234"}' -H 'Content-Type: application/json'
# 响应不得含 pin_hash
```

## 关联

- ADR 0001：SQLite 存储能力验证与存储决策
- REQUIREMENTS 第11章（家庭内用户与角色）
- AGENTS.md 依赖方向 / domain 层规范

## 补丁 v2（2026-09-27）：tag 规则细化

### 修正
原 ADR 0002 说"domain 层不做 JSON 序列化"——表述过宽，被解读为"domain 禁一切 tag"。本补丁细化：

- ❌ **禁止 `json` tag**：防序列化泄漏（PinHash）、防 API 契约与 DB 契约耦合
- ✅ **允许 `db` tag**：sqlx 反射映射用，无语义风险、无序列化影响
- ✅ **允许 `validate` tag**（可选）：service 层入参校验

### 理由

`json` tag 与 `db` tag 的风险不等价：

| 维度 | `json` tag | `db` tag |
|------|-----------|----------|
| 参与序列化 | ✅（泄漏风险） | ❌ |
| 影响 API 契约 | ✅（多视角问题） | ❌ |
| 换存储层时需改 | — | 否（列名两库一致） |

禁 `json` tag 有实际理由；禁 `db` tag 是教条。

### 结构简化

**不引入 PO 层。** `domain` 实体直接做 sqlx 扫描目标：

```
internal/
├── domain/kid.go          # 有 db tag；无 json tag
├── service/kid_service.go
└── api/
    ├── dto/kid.go           # json tag；多视角契约
    └── kid_handler.go       # domain → DTO 转换
```

**DTO 保留**（ADR 0002 原核心成果不变）：`pin_hash` 不泄漏、三视角契约，靠的是 DTO 而非 PO。

### 为什么放弃 PO

- PO 对"换存储层"的贡献 ≈ 0——靠的是仓储接口 + 依赖注入（Day 4 已做）
- 15 张表 × 2 转换函数 ≈ 600 行样板
- 每加一列需改 5 处（domain/PO/ToDomain/FromDomain/SQL）
- 单机 Box 场景下，PO 的"domain 绝对纯净"收益 ≈ 0

### 数据访问层

- **`database/sql` + `jmoiron/sqlx`**：sqlx 只做 scan 简化，所有 SQL 手写
- 禁用 sqlx 的 ORM 类特性；只用 `GetContext` / `SelectContext` / `NamedExec` / `In`
