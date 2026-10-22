# ============================================================
# 开发常用命令
# 依赖：make、docker（含 compose v2）、Go 1.26+
# 环境变量来自 deploy/.env（由 deploy/.env.example 复制，不进版本库）
# ============================================================

COMPOSE      := docker compose -f deploy/docker-compose.yml
ENV_FILE     ?= deploy/.env
MYSQL_HOST_PORT ?= 3307
MYSQL_USER   ?= blog
MYSQL_PASS   ?= blog_pass_change_me
MYSQL_DB     ?= blog
MYSQL_HOST   ?= 127.0.0.1
DSN          := mysql://$(MYSQL_USER):$(MYSQL_PASS)@tcp($(MYSQL_HOST):$(MYSQL_HOST_PORT))/$(MYSQL_DB)
MIGRATE_IMG  := migrate/migrate:v4.17.0

.DEFAULT_GOAL := help
.PHONY: help up up-infra up-full down ps logs clean env \
        migrate-up migrate-down migrate-version \
        openapi-gen wire fmt vet test test-race build run admin-create

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------- 环境 ----------
env: ## 从示例生成本地环境文件（已存在则拒绝覆盖）
	@test -f $(ENV_FILE) && echo "$(ENV_FILE) 已存在，未覆盖" || \
		(cp deploy/.env.example $(ENV_FILE) && echo "已生成 $(ENV_FILE)，请填入真实密码")

# ---------- 基础设施 ----------
up: ## 起 mysql + blog-redis（阶段 0/1 只需这两个）
	$(COMPOSE) up -d mysql blog-redis

up-infra: ## 起全部基础设施（含 meilisearch、minio）
	$(COMPOSE) up -d mysql blog-redis meilisearch minio

up-full: ## 起全部服务（含 app、web、nginx，需先构建镜像）
	$(COMPOSE) --profile full up -d

down: ## 停止全部服务（保留数据卷）
	$(COMPOSE) --profile full down

ps: ## 查看服务状态
	$(COMPOSE) ps

logs: ## 跟踪 mysql 与 blog-redis 日志
	$(COMPOSE) logs -f mysql blog-redis

clean: ## 停止并删除数据卷（危险：数据全部丢失）
	$(COMPOSE) --profile full down -v

# ---------- 迁移 ----------
# 通过容器执行，本地无需安装 migrate
migrate-up: ## 执行全部迁移
	docker run --rm -v "$(CURDIR)/migrations:/migrations" $(MIGRATE_IMG) \
		-path=/migrations -database "$(DSN)" up

migrate-down: ## 回滚一个版本
	docker run --rm -v "$(CURDIR)/migrations:/migrations" $(MIGRATE_IMG) \
		-path=/migrations -database "$(DSN)" down 1

migrate-version: ## 查看当前迁移版本
	docker run --rm -v "$(CURDIR)/migrations:/migrations" $(MIGRATE_IMG) \
		-path=/migrations -database "$(DSN)" version

# ---------- 代码生成 ----------
openapi-gen: ## 从 openapi.yaml 生成 Go DTO
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
		-config api/oapi-codegen.yaml api/openapi.yaml

wire: ## 生成依赖装配代码
	go run github.com/google/wire/cmd/wire@v0.7.0 ./internal/bootstrap

# ---------- 代码质量 ----------
fmt: ## 格式化
	gofmt -w ./cmd ./internal

vet: ## 静态检查
	go vet ./...

test: ## 运行测试
	go test ./... -count=1

test-race: ## 竞态检测（需要 64 位 cgo 工具链，Windows 下需装 64 位 gcc）
	go test ./... -race -count=1

build: ## 构建二进制
	go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

run: ## 本地运行（读取 deploy/.env）
	go run ./cmd/server -env-file $(ENV_FILE)

admin-create: ## 创建或提升管理员（-env-file 读取部署配置）
	go run ./cmd/admincreate -env-file $(ENV_FILE) \
		-username "$(USERNAME)" -email "$(EMAIL)" -password "$(PASSWORD)"
