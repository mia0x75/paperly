# Paperly Box — Makefile
# 默认构建产物输出到 bin/ 目录。技术栈口径见 TECH_STACK.md 第 3 章。

BINARY      := paperly
BIN_DIR     := bin
ENTRY       := ./cmd/server
GO          := go
GOFLAGS     :=
LDFLAGS     := -s -w

# 版本信息注入（可用 make build VERSION=1.2 覆盖）
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_TIME  ?= $(shell date "+%Y-%m-%d %H:%M:%S")
LDFLAGS     += -X main.version=$(VERSION) -X \"main.buildTime=$(BUILD_TIME)\"

.DEFAULT_GOAL := build

.PHONY: build run dev test vet lint fmt tidy clean frontend help

## build: 编译前端 + 后端到 bin/（前端产物经 go:embed 打入单二进制）
build: export CGO_ENABLED=0
build: export GOAMD64_VERSION=v3
build: frontend
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(ENTRY)

## build-backend: 仅编译后端（不含前端；需已有 internal/web/dist 产物）
build-backend:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(ENTRY)

## run: 直接运行后端（不产出二进制）
run:
	$(GO) run $(ENTRY)

## dev: air 热重载（需先安装 air）
dev:
	air

## test: 运行全部测试
test:
	$(GO) test ./...

## vet: go vet 静态检查
vet:
	$(GO) vet ./...

## lint: golangci-lint 检查
lint:
	golangci-lint run

## fmt: gofmt 格式化
fmt:
	gofmt -w .

## tidy: 整理 go.mod / go.sum
tidy:
	$(GO) mod tidy

## clean: 清理构建产物（保留 ./data 数据目录）
clean:
	rm -rf $(BIN_DIR)

## frontend: 构建前端产物到 internal/web/dist（Bun，供 go:embed）
frontend:
	cd frontend && bun run build

## help: 显示可用目标
help:
	@awk -F': ' '/^## [a-zA-Z_-]+:/ {sub(/^## /, "", $$1); printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
