SHELL := /bin/bash
VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")
LDFLAGS := -X main.Version=$(VERSION)

# 本地开发数据库连接串，可用环境变量覆盖
EAGLE_DSN ?= postgres://eagle:eagle@127.0.0.1:5432/eagle?sslmode=disable

.PHONY: init
# 安装开发期工具链
init:
	go install github.com/google/wire/cmd/wire@v0.7.0
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/pressly/goose/v3/cmd/goose@v3.27.3
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

.PHONY: api
# 生成对外契约代码（api/ 下的 proto）
api:
	buf generate --template buf.gen.yaml

.PHONY: config
# 生成各服务内部配置代码（app/*/internal/conf）
config:
	buf generate --template buf.gen.config.yaml

.PHONY: lint-proto
# proto 风格检查 + 兼容性检查（against main）
lint-proto:
	buf lint
	buf breaking --against '.git#branch=main'

.PHONY: sqlc
# 由 db/query/*.sql 生成类型安全的数据访问代码
sqlc:
	cd db && sqlc generate

.PHONY: wire
# 生成依赖注入代码
wire:
	cd app/system/cmd/server && wire
	cd app/auth/cmd/server && wire

.PHONY: migrate-up
# 执行数据库迁移
migrate-up:
	goose -dir db/migrations postgres "$(EAGLE_DSN)" up

.PHONY: migrate-down
# 回滚一个版本
migrate-down:
	goose -dir db/migrations postgres "$(EAGLE_DSN)" down

.PHONY: migrate-status
migrate-status:
	goose -dir db/migrations postgres "$(EAGLE_DSN)" status

.PHONY: generate
# 全量生成：proto + 配置 + sqlc + wire
generate: api config sqlc wire
	go mod tidy

.PHONY: build
# 编译所有服务到 bin/
build:
	mkdir -p bin/
	go build -ldflags "$(LDFLAGS)" -o ./bin/ ./app/...

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: test
# 单测（biz 层）+ 集成测试（testcontainers 起真 PG）
test:
	go test -race -cover ./...

.PHONY: up
# 起本地依赖（PostgreSQL + Redis + OTel + Grafana）
up:
	docker compose -f deploy/docker-compose.yml up -d

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
