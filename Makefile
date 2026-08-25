SHELL := /bin/bash
.DEFAULT_GOAL := help
SPEC := api/openapi/openapi.yaml

.PHONY: help doctor build test lint gen gen-check clean

help: ## 列出可用目标与说明
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n",$$1,$$2}'

doctor: ## 工具链自检（go/pnpm/docker/oapi-codegen/spectral…）
	@bash scripts/doctor.sh

build: ## 构建全部（Go 二进制 + 前端产物；未就绪部分自动跳过并提示）
	@bash scripts/make-part.sh build

test: ## 运行全部测试（go test + web 单测）
	@bash scripts/make-part.sh test

lint: ## 运行全部静态检查（golangci-lint + eslint/oxlint）
	@bash scripts/make-part.sh lint

lint-api: ## 闸① spec lint（Spectral，§12.4）
	@npx --yes @stoplight/spectral-cli@6 lint api/openapi/openapi.yaml --ruleset .spectral.yaml --fail-severity=error

gen: ## 从 api/openapi 再生 api/gen（唯一再生途径，禁止手改）
	@bash scripts/gen-openapi.sh

gen-check: ## 校验 api/gen 与重新生成结果一致（CI 门禁用，diff 非空即失败）
	@bash scripts/gen-openapi.sh --check

clean: ## 清理构建产物
	@rm -rf bin cmd/control-plane/webui/dist
	@echo "cleaned"
