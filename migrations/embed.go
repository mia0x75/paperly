// Package migrations 以 go:embed 方式内嵌仓库根目录的 schema 迁移脚本，
// 供 repository 层按序执行。文件名即版本顺序（0001_..., 0002_...）。
package migrations

import "embed"

// FS 内嵌 migrations/ 下的全部 .sql 文件。
//
//go:embed *.sql
var FS embed.FS
