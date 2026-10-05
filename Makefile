GO       ?= go
NPM      ?= npm
COMPOSE  ?= docker compose
BACKEND  ?= backend
FRONTEND ?= frontend
API_URL  ?= http://127.0.0.1:8080

.DEFAULT_GOAL := help
.PHONY: help deps deps-backend env fmt vet build build-backend build-frontend \
        test test-backend test-unit test-e2e test-live test-frontend check check-frontend verify \
        backend frontend start-frontend dev \
        migrate migrate-down \
        docker-build up docker-down docker-logs docker-ps health clean

# --- подготовка окружения ---

## help: показать список целей
help:
	@echo "marketing-agents — доступные цели (make <цель>):"
	@echo
	@grep -hE '^## ' $(MAKEFILE_LIST) \
		| sed -E 's/^## ([a-zA-Z_-]+): /\1|/' \
		| sort \
		| awk -F'|' '{printf "  \033[36m%-17s\033[0m %s\n", $$1, $$2}'
	@echo
	@echo "Переменные: GO=$(GO)  NPM=$(NPM)  COMPOSE=$(COMPOSE)  API_URL=$(API_URL)"

## deps: поставить зависимости бэкенда и фронта
deps: deps-backend
	cd $(FRONTEND) && $(NPM) ci

deps-backend:
	cd $(BACKEND) && $(GO) mod download

## env: создать backend/.env из примера, если файла ещё нет
env:
	@test -f $(BACKEND)/.env || { \
		cp $(BACKEND)/.env.example $(BACKEND)/.env; \
		echo "создан $(BACKEND)/.env — впишите DEEPSEEK_API_KEY (и BASIC_AUTH_* для публикации)"; \
	}

# node_modules — файловая цель: npm ci выполняется только когда менялись
# package.json или lock-файл. Нужные для тестов/сборки артефакты SvelteKit
# (node_modules/$app) генерирует сам `npm test` через svelte-kit sync.
$(FRONTEND)/node_modules: $(FRONTEND)/package.json $(FRONTEND)/package-lock.json
	cd $(FRONTEND) && $(NPM) ci

# --- код ---

## fmt: форматировать Go-код (gofmt -w)
fmt:
	cd $(BACKEND) && gofmt -l -w .

## vet: статические проверки бэкенда (go vet)
vet:
	cd $(BACKEND) && $(GO) vet ./...

## build: проверить сборку бэкенда и собрать фронт
build: build-backend build-frontend

build-backend:
	cd $(BACKEND) && $(GO) build ./...

## build-frontend: собрать фронт в frontend/build (Node-сервер)
build-frontend: $(FRONTEND)/node_modules
	cd $(FRONTEND) && $(NPM) run build

## test: тесты бэкенда и фронта
test: test-backend test-frontend

## test-backend: go test по всем пакетам (internal + tests/e2e + tests/live)
test-backend:
	cd $(BACKEND) && $(GO) test ./...

## test-unit: только юнит-тесты пакетов internal (без сквозных)
test-unit:
	cd $(BACKEND) && $(GO) test ./internal/...

## test-e2e: сквозные тесты (стор → трасса → оркестратор → раннер)
test-e2e:
	cd $(BACKEND) && $(GO) test ./tests/e2e/...

## test-live: дымовой тест против живого MCP (ходит в сеть и тратит квоту; нужны WORDSTAT_MCP_*)
test-live:
	cd $(BACKEND) && $(GO) test ./tests/live/ -run TestLiveMCP -v

## test-frontend: юнит-тесты фронта (vitest)
test-frontend: $(FRONTEND)/node_modules
	cd $(FRONTEND) && $(NPM) test

## check: типы и статические проверки (go vet + svelte-check)
check: vet check-frontend

check-frontend: $(FRONTEND)/node_modules
	cd $(FRONTEND) && $(NPM) run check

## verify: полный набор перед коммитом (build + check + test)
verify: build check test

# --- миграции схемы (golang-migrate) ---
# Приложение миграции не применяет: в compose это отдельный сервис migrate,
# локально — эти цели. Сервер на старте только проверяет версию схемы.

## migrate: применить миграции схемы (golang-migrate, up)
migrate: env
	cd $(BACKEND) && $(GO) run ./cmd/migrate up

## migrate-down: откатить последнюю миграцию
migrate-down: env
	cd $(BACKEND) && $(GO) run ./cmd/migrate down

# --- локальный запуск (без Docker) ---

## backend: API на 127.0.0.1:8080 (окружение читается из backend/.env)
backend: migrate
	cd $(BACKEND) && $(GO) run ./cmd/server

## frontend: dev-сервер фронта на 127.0.0.1:5173 (/api и /healthz → :8080)
frontend:
	cd $(FRONTEND) && $(NPM) run dev -- --host 127.0.0.1

## start-frontend: прод-сервер фронта из build/ на 127.0.0.1:3000
start-frontend: build-frontend
	cd $(FRONTEND) && BACKEND_URL=$(API_URL) $(NPM) start

## dev: миграции, затем API и dev-сервер фронта вместе (Ctrl-C гасит оба)
dev: migrate
	@echo "API → http://127.0.0.1:8080    UI → http://127.0.0.1:5173    (Ctrl-C — остановить)"
	@trap 'kill 0' INT TERM; \
		( set -a; . $(BACKEND)/.env; set +a; cd $(BACKEND) && $(GO) run ./cmd/server ) & \
		( cd $(FRONTEND) && $(NPM) run dev -- --host 127.0.0.1 ) & \
		wait

# --- Docker Compose ---

## docker-build: собрать образы
docker-build:
	$(COMPOSE) build

## up: поднять стек compose (сервис migrate применяет схему, затем API; фронт на 127.0.0.1:8080)
up:
	$(COMPOSE) up -d --build

## down: остановить и удалить контейнеры стека
down:
	$(COMPOSE) down

## logs: смотреть логи стека
logs:
	$(COMPOSE) logs -f

## docker-ps: состояние сервисов стека
docker-ps:
	$(COMPOSE) ps

## health: проверить /healthz (локальный API или стек compose)
health:
	@printf "healthz: "; curl -fsS --max-time 5 http://127.0.0.1:8080/healthz; echo

# --- прочее ---

## clean: удалить артефакты сборки фронта
clean:
	rm -rf $(FRONTEND)/build $(FRONTEND)/.svelte-kit
