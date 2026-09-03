SHELL := /bin/bash
VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
LDFLAGS := -X main.Version=$(VERSION)

# 业务代码是单一 module；tools 独立成模块，用来锁定生成工具链版本，
# 不让 buf/goose/golangci-lint/wire 的依赖污染业务依赖图。
MODULES := . tools
REGISTRY ?= eagle
IMAGE ?= $(REGISTRY)/eagle
EAGLE_BUILDER_IMAGE ?= mirror.gcr.io/library/golang:1.27-alpine
EAGLE_RUNTIME_IMAGE ?= gcr.io/distroless/static-debian12:nonroot

# 工具从 tools module 编译成二进制后在仓库根目录执行。
# 不用 `go -C tools tool xxx`：那会把工作目录切到 tools/，
# buf.gen.yaml 里的 `directory: api`、goose 的 -dir、wire 的包路径都会解析错。
BIN := $(CURDIR)/bin
BUF := $(BIN)/buf
ENT := $(BIN)/ent
GOOSE := $(BIN)/goose
GOLANGCI_LINT := $(BIN)/golangci-lint
WIRE := $(BIN)/wire

TOOL_PKG_buf := github.com/bufbuild/buf/cmd/buf
TOOL_PKG_ent := entgo.io/ent/cmd/ent
TOOL_PKG_goose := github.com/pressly/goose/v3/cmd/goose
TOOL_PKG_golangci-lint := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
TOOL_PKG_wire := github.com/google/wire/cmd/wire

$(BIN)/%:
	@mkdir -p $(BIN)
	go -C tools build -o $@ $(TOOL_PKG_$*)

EAGLE_DSN ?= postgres://eagle:eagle@127.0.0.1:5432/eagle?sslmode=disable
MIGRATION_DIR := migrations

.PHONY: init
# 下载并验证项目锁定的开发期工具链
init: $(BUF) $(GOOSE) $(GOLANGCI_LINT) $(WIRE)
	$(BUF) --version
	$(GOOSE) -version
	$(GOLANGCI_LINT) --version

.PHONY: api
# 生成对外契约代码（api/ 下的 proto）
api: $(BUF)
	$(BUF) generate --template buf.gen.yaml

.PHONY: config
# 生成进程配置代码（pkg/platform/config）
config: $(BUF)
	$(BUF) generate --template buf.gen.config.yaml

.PHONY: lint-proto
# proto 风格检查 + 兼容性检查（against master）
lint-proto: $(BUF)
	$(BUF) lint
	$(BUF) breaking api --against '.git#branch=master,subdir=api'

.PHONY: ent
# 生成 Ent 数据访问代码。--target 省略时默认取 schema 目录的父目录，
# 即 internal/platform/database/ent。
ent: $(ENT)
	$(ENT) generate --feature sql/execquery,sql/upsert ./internal/platform/database/ent/schema

.PHONY: tidy
tidy:
	@for module in $(MODULES); do \
		go -C $$module mod tidy || exit 1; \
	done

.PHONY: migrate-up
# 执行数据库迁移
migrate-up: $(GOOSE)
	$(GOOSE) -dir $(MIGRATION_DIR) postgres "$(EAGLE_DSN)" up

.PHONY: migrate-down
# 回滚一个版本
migrate-down: $(GOOSE)
	$(GOOSE) -dir $(MIGRATION_DIR) postgres "$(EAGLE_DSN)" down

.PHONY: migrate-status
migrate-status: $(GOOSE)
	$(GOOSE) -dir $(MIGRATION_DIR) postgres "$(EAGLE_DSN)" status

.PHONY: wire
# 生成组合根的 Wire 注入代码
wire: $(WIRE)
	$(WIRE) ./cmd/eagle

.PHONY: generate
# 全量生成：对外契约 + 进程配置 + Ent + Wire
generate: api config ent wire tidy

.PHONY: build
# 编译服务与迁移工具到 bin/
build:
	@mkdir -p $(BIN)
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/eagle ./cmd/eagle
	go -C tools build -o $(BIN)/migrate ./migrate

.PHONY: image
# 构建服务镜像，例如 make image VERSION=v1.2.0 REGISTRY=registry.example.com/eagle
image:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILDER_IMAGE=$(EAGLE_BUILDER_IMAGE) \
		--build-arg RUNTIME_IMAGE=$(EAGLE_RUNTIME_IMAGE) \
		-t $(IMAGE):$(VERSION) .

.PHONY: push-image
# 推送服务镜像；生产发布时显式执行，不绑定到 build
push-image:
	docker push $(IMAGE):$(VERSION)

.PHONY: run
# 本地直接启动服务（依赖 make up-deps 起好的 PostgreSQL）
run:
	EAGLE_DATABASE_DSN="$(EAGLE_DSN)" go run -ldflags "$(LDFLAGS)" ./cmd/eagle -conf configs

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

.PHONY: test
# 单测 + 集成测试（embedded-postgres，不需要 Docker）
test:
	go test -race -cover ./...

.PHONY: up
# 构建并启动完整本地环境
up:
	EAGLE_BUILDER_IMAGE="$(EAGLE_BUILDER_IMAGE)" \
	EAGLE_RUNTIME_IMAGE="$(EAGLE_RUNTIME_IMAGE)" \
		docker compose -f deploy/docker-compose.yml up -d --build

.PHONY: up-deps
# 只启动本地基础依赖，服务由 make run 单独启动
up-deps:
	docker compose -f deploy/docker-compose.yml up -d postgres

.PHONY: validate-deploy
# 校验 Compose 文件能被正确解析
validate-deploy:
	docker compose -f deploy/docker-compose.yml config --quiet
	EAGLE_IMAGE=eagle/eagle:validation \
	EAGLE_ENV_FILE=$(CURDIR)/deploy/environments/development.env.example \
		docker compose -f deploy/compose.app.yml config --quiet
	sh -n deploy/scripts/deploy.sh

.PHONY: down
down:
	docker compose -f deploy/docker-compose.yml down

.PHONY: all
all: generate build

.PHONY: help
help:
	@awk '/^[a-zA-Z\-\_0-9]+:/ { \
		helpMessage = match(lastLine, /^# (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 0, index($$1, ":")); \
			helpMessage = substr(lastLine, RSTART + 2, RLENGTH); \
			printf "\033[36m%-18s\033[0m %s\n", helpCommand, helpMessage; \
		} \
	} { lastLine = $$0 }' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help
