# ADR 0003: 试卷列表分页改为 keyset 游标

## 状态

已接受（人类裁定，2026-10-01）。同步修订 `DATABASE_DESIGN.md` 6.2 节。

## 背景

`GET /api/exams` 原用 `page/page_size`（LIMIT ? OFFSET ?）分页。在真实使用中暴露：

1. **边翻边删/增会漏行或重复行**：OFFSET 是**位置切分**，前面行被删或被插入会让后续窗口整体位移。首页历史表与列表页都可能在上传/删除后立即翻页，用户会看到某条记录凭空消失或出现两次。
2. **深翻页代价线性增长**：`OFFSET N` 需扫描并丢弃前 N 行，页数越深越慢。
3. **与"最新在前"的心智一致**：列表本就是按 `id DESC` 展示最新，位置分页对这个顺序冗余。

## 决策

试卷列表分页改为 **keyset 游标**：

- 请求：`GET /api/exams?...&cursor=<id>&limit=20`。
  - `cursor` = 上一页最后一条的 `id`；省略或 0 = 从最新开始。
  - `limit` = 单页条数，默认 20，上限 100。
- SQL：`WHERE id < :cursor ORDER BY id DESC LIMIT :limit+1`。
  - `+1` 多取一条探测是否还有下一页，避免额外的 COUNT。
- 响应：`{ items, next_cursor, has_more }`。
  - `next_cursor = 0` ⟺ `has_more = false`。
  - **不返回 `total`**：游标分页不提供精确总数（要总数就得每次 COUNT，抵消 keyset 的意义）。

## 理由

1. `id` 单调自增且是排序键，`id < cursor` 在增删下天然稳定——不再漏行/重复。
2. 游标列即排序键，可命中主键，深翻页恒定为 `LIMIT` 级别代价。
3. 前端"上一页"用**游标栈**实现（keyset 无法反向直达，需记录来路）；这是已知取舍，换来的是正确性与性能。

## 影响

- 前端 `ExamListPage` 改游标栈；`ExamTableCard`（首页最近 10 份）改用 `has_more` 表达"还有更多"，不再显示精确总数。
- 仓储 `ExamRepository.List` 签名由 `(items, total, err)` 改为 `(items, nextCursor, err)`。
- 其余列表接口（kids 等，当前返回数组不分页）不受影响；未来新增加的分页列表默认采用本方案。
