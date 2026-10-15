# PoC：modernc.org/sqlite 存储能力验证

配合 `docs/adr/0001-sqlite-storage-decisions.md` 使用。目标：回答「连接池怎么配」「备份怎么做」
「乐观版本号怎么实现」，作为开始建表的前提。

## 运行方式

每项 PoC 是**独立可运行**的 `package main`，平铺在 `poc/` 下：

```bash
go run ./poc/01_sql_dialect.go
go run ./poc/08_file_size.go --mb=256   # 缩数据量调试
```

## 为什么每个文件都有 `//go:build poc`

`poc/` 下 9 个文件均为 `package main` 且各自定义了 `main()`/`printResult()`。
若不加构建标签，`go build ./...` 会因「同目录重复声明」失败。
因此所有 PoC 文件带 `//go:build poc`，默认被 Go 工具链忽略：

- 正常运行主程序 `go build ./...` 不受影响。
- 运行 PoC 时用显式文件路径 `go run ./poc/0X_xxx.go`（`go run <file>` 会忽略构建约束）。
- 由于每个文件各自定义 `main()`/`printResult()`，**不可**用 `go vet ./poc/...` 一次性检查
  （会报重复声明）。逐文件检查：`for f in poc/*.go; do go vet "$f"; done`。

## 输出格式

每项统一输出一行结果，便于脚本聚合：

```
[PASS] 项目名 | 实测值 | 备注
[FAIL] 项目名 | 原因
```

- 使用 `os.MkdirTemp` 创建独立临时目录，退出时清理。
- 不依赖 `./data`，不污染开发库。

## 与需求文档的对应

| # | PoC | REQUIREMENTS 对应 |
|---|-----|-------------------|
| 1 | SQL 方言 | 章节2.1-1 |
| 2 | 自增主键 | 章节2.1-2 |
| 3 | 复合唯一约束 | 章节2.1-3 |
| 4 | 索引 | 章节2.1-4 |
| 5 | JSON 函数 | 章节2.1-5 |
| 6 | 单文件并发 | 章节2.1-7 |
| 7 | 备份 | 章节2.1-8、章节8.2 |
| 8 | 单文件上限 | 章节2.1-9 |
| 9 | 乐观版本号 | 章节2.1-10 |
