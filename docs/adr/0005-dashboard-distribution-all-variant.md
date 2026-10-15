# ADR 0005: 家长首页「试卷分布」卡默认看全部孩子，新增 All 聚合口径

## 状态

已接受（人类裁定，2026-10-09）。

## 背景

家长首页 ROW3「试卷分布」与 ROW4「最新试卷」原先只呈现**当前孩子**的数据：
`DashboardPanels` 把顶栏选中的 `kidId` 透传给两张卡，`SubjectDonut` 直接用
`dashboard.distribution`（kid 收窄）。需求要求家长能一屏看到**全部孩子**的概览，
并在每张卡上按需下钻到某个孩子。

`internal/domain/dashboard.go` 的仓储契约注释曾明确写下：

- `SubjectDistributionForKid` 等是 **kid 收窄**；
- 「**没有** All 变体，也不该加——加了就会诱导 UI 把全体数据挂到单个孩子的页面上」。

即原决策是「dashboard 的分布口径只服务单个孩子」。本次需求直接与之冲突。

## 决策

人类裁定采用**卡片级范围选择**，并为此打破上述「无 All 变体」约束：

1. **新增可聚合口径**：`KidDashboardRepository.SubjectDistributionAll(ctx)`，
   返回**全部孩子聚合**的科目分布（不按 `kid_id` 过滤），口径与
   `SubjectDistributionForKid` 完全一致（只含有归档的学科，软删卷不计入）。
2. **接口按 kid_id 可选暴露**：`GET /api/dashboard/distribution?kid_id=<可选>`。
   - 省略 `kid_id` → 走 `SubjectDistributionAll`（「全部」）；
   - 带 `kid_id` → 走 `SubjectDistributionForKid`，且经 `LoadKidForDashboard`
     校验（不存在 / 已毕业 / 已删 → 404），与其它 dashboard 端点的降级一致。
3. **卡片级范围，不联动全局**：分布卡、最新试卷卡各自持有独立的 `kidId`
   （`null` = 全部），默认全部。切换只重拉本卡，不改顶栏当前孩子、不重拉整页
   聚合、不影响 KPI。最新试卷「全部」无需后端改动（`examApi.list()` 省略
   `kid_id` 即返回全部），仅有筛选器。
4. **kid 端不变**：孩子端 `/kid` 仍走 `dashboard.distribution`（JWT 收窄），
   不渲染 `DistributionCard`、无筛选器。三视角 DTO 约定不受影响——
   `SubjectDistributionAll` 只服务家长端点。

## 理由

1. 「家长看全部 + 按孩子下钻」是卡片级范围，不是「把全体数据塞进某个孩子的
   页面」。原约束针对的是后者（kid 页不应出现全体分母）；本 ADR 用
   **显式的 All 变体 + 可选 kid_id 参数**把两个口径分开，语义不混。
2. 复用既有的 kid 收窄查询与 `withPercent` 合成，All 变体只差一个 `WHERE`，
   改动面最小。
3. 只给 `distribution` 加 All 变体，**不给** `UploadSeriesForKid` 加——保持
   「无必要不扩面」的边界；最新试卷的全部口径由既有 exam-list 端点天然满足。

## 后果

- 新增仓储方法 `SubjectDistributionAll`、服务方法
  `DashboardService.Distribution(ctx, kidID *int64)`、DTO
  `DistributionResponse`、路由 `GET /api/dashboard/distribution`；API 文档需同步。
- `dashboard.go` 的 scoping 注释改述为「kid 收窄」与「可聚合」两类，并指向本 ADR。
- 前端新增 `KidScopeFilter.vue`（卡片级「全部 / 孩子」下拉）与
  `DistributionCard.vue`（家长端自取自渲染）；`SubjectDonut` 增加 `action` 插槽
  与 `error`/`retry`；`ExamTableCard` 去掉 `kidName`、增加 `scopeKidId`。
- 与品牌隔离、枚举、三视角 DTO、keyset 分页规范无冲突。
