# TokenRouter 的开发、检查和构建入口。CI 的每个 job 调用这里的同名目标。
export GOTOOLCHAIN := go$(shell sed -n 's/^go //p' backend/go.mod)
export PYTHONDONTWRITEBYTECODE := 1

# pnpm 版本由 frontend/package.json 的 packageManager 字段决定。
PNPM := pnpm --dir frontend
GOLANGCI := cd backend && bash ../tools/golangci-lint.sh
# 带 integration 构建约束的包依赖 Docker，test-integration 只编译和运行这些包。
INTEGRATION_PKGS = $(shell cd backend && grep -rlE '^//go:build integration$$' --include='*.go' . | xargs -n1 dirname | sort -u)

.PHONY: build build-frontend build-backend generate \
	fmt fmt-check lint lint-go lint-frontend \
	test test-go test-frontend test-integration test-embed test-scripts test-installer test-tools test-e2e \
	check verify security security-go security-frontend hooks

# 先打包前端，再编译嵌入前端资源的发布形态服务端。
build: build-frontend
	cd backend && CGO_ENABLED=0 go build -tags=embed -trimpath -o bin/server ./cmd/server

build-frontend:
	$(PNPM) run build:assets

# 编译不含前端资源的后端，开发时配合 Vite 使用。
build-backend:
	$(MAKE) -C backend build

generate:
	$(MAKE) -C backend generate

# 格式化改动的手写 Go 文件。fmt-check 传入 BASE 时比较 BASE 到 HEAD 的提交。
fmt:
	python3 tools/format_go.py

fmt-check:
	python3 tools/format_go.py --check $(if $(BASE),--base "$(BASE)")

lint: lint-go lint-frontend

# integration 标签同时覆盖无标签文件和集成测试文件，embed 文件由 test-embed 检查。
lint-go:
	cd tools/architecture && GOWORK=off go test -count=1 ./...
	$(GOLANGCI) config verify
	$(GOLANGCI) run --build-tags=integration ./...

lint-frontend:
	$(PNPM) run lint:check
	$(PNPM) run typecheck

test: test-go test-frontend

test-go:
	cd backend && go test ./...

test-frontend:
	$(PNPM) run test:run

# 包级并发限制为 4，控制同时运行的 PostgreSQL 和 Redis 容器数量。
test-integration:
	cd backend && go test -tags=integration -p=4 $(INTEGRATION_PKGS)

# embed 构建读取 build-frontend 生成的 dist，.keep 占位文件不能代替生产资源。
test-embed:
	@test -s backend/internal/web/dist/index.html || { echo '缺少前端构建产物，请先运行 make build-frontend'; exit 1; }
	$(GOLANGCI) run --build-tags=embed ./internal/web/... ./cmd/server/...
	cd backend && go test -tags=embed ./internal/web/...

test-scripts:
	/bin/bash -n deploy/apple-container.sh deploy/install.sh
	/bin/bash deploy/tests/install-github-token-test.sh
	/bin/bash deploy/tests/apple-container-test.sh
	/bin/sh deploy/tests/docker-compose-security-test.sh
	/bin/sh deploy/tests/docker-compose-postgres-test.sh
	/bin/sh deploy/tests/docker-compose-variants-test.sh
	/bin/sh deploy/tests/docker-compose-gateway-env-test.sh
	/bin/sh deploy/tests/docker-runtime-resources-test.sh
	/bin/sh deploy/test-caddyfile-cache.sh
	/bin/sh tools/goreleaser_prebuilt_test.sh

# Linux 安装器依赖 Bash 4+ 和 sha256sum，macOS 上借 Ubuntu 容器运行。
test-installer:
	@if [ "$$(uname -s)" = Linux ]; then bash deploy/tests/install-brand-test.sh; else docker run --rm -v "$(CURDIR):/source:ro" -w /source ubuntu:24.04 bash deploy/tests/install-brand-test.sh; fi

# 仓库工具的单元测试，格式化测试需要 golangci-lint。
test-tools:
	python3 -m unittest discover -s tools -p 'test_*.py'
	cd tools/declorder && GOWORK=off go test ./...

# 外部 E2E 测试连接已运行的服务，地址和凭据从环境变量读取。
test-e2e:
	cd backend && go test -count=1 -tags=e2e -v -timeout=300s ./tests/integration/...

# 推送前快检，pre-push hook 调用同一个脚本。BASE 默认取上游分支的共同祖先。
check:
	python3 tools/check_changed.py $(if $(BASE),--base "$(BASE)")

# 本地跑完 CI 的全部检查，需要 Docker。
verify: lint test-tools test test-integration build test-embed test-scripts test-installer

security: security-go security-frontend

security-go:
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@$$(cat ../.govulncheck-version) ./...

security-frontend:
	$(PNPM) install --frozen-lockfile
	python3 tools/check_pnpm_audit_exceptions.py --run --exceptions .github/audit-exceptions.yml

hooks:
	git config --local core.hooksPath .githooks
