GO       ?= go
NPM      ?= npm
COMPOSE  ?= docker compose
BACKEND  ?= backend
FRONTEND ?= frontend
API_URL  ?= http://127.0.0.1:8080

# dburl loads backend/.env directly and encodes credentials; no shell evaluation.
.DEFAULT_GOAL := help
.PHONY: help deps deps-backend env fmt vet build build-backend build-frontend \
        test test-backend test-unit test-e2e test-live test-frontend check check-frontend verify \
        sync-skills \
        backend frontend start-frontend dev \
        db-up db-down migrate migrate-down \
        docker-build up docker-down docker-logs docker-ps health clean

# run-tests: поднимает MariaDB (если есть Docker) и запускает go test. Без Docker
# тесты с MariaDB пропускаются с понятным сообщением, остальные выполняются.
define run_go_tests
	@if docker info >/dev/null 2>&1; then \
		$(COMPOSE) up -d --wait mariadb >/dev/null && \
		cd $(BACKEND) && TEST_DATABASE_URL="$${TEST_DATABASE_URL:-$$($(GO) run ./cmd/dburl -test)}" $(GO) test $(1); \
	else \
		echo "Docker недоступен: тесты с MariaDB пропущены (нужна make db-up)"; \
		cd $(BACKEND) && $(GO) test $(1); \
	fi
endef

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

## sync-skills: скопировать скилы пакета marketing-skills во встроенные копии бэкенда
sync-skills:
	@for d in marketing-skills/skills/*/; do \
		name=$$(basename $$d); \
		mkdir -p $(BACKEND)/internal/adapters/skills/prompts/$$name; \
		cp $$d/SKILL.md $(BACKEND)/internal/adapters/skills/prompts/$$name/SKILL.md; \
	done
	@echo "скилы синхронизированы: $(BACKEND)/internal/adapters/skills/prompts"

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

## test-backend: go test по всем пакетам (internal + tests/e2e + tests/live) на живой MariaDB
test-backend:
	$(call run_go_tests,./...)

## test-race: race detector для runner, trace и конкурентных DB проверок
test-race:
	$(call run_go_tests,-race ./internal/application/... ./internal/features/trace/... ./tests/repository/... ./tests/e2e/...)

## test-unit: только юнит-тесты пакетов internal (без сквозных)
test-unit:
	$(call run_go_tests,./internal/...)

## test-e2e: сквозные тесты (стор → трасса → оркестратор → раннер)
test-e2e:
	$(call run_go_tests,./tests/e2e/...)

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

# --- БД и миграции схемы (MariaDB + готовый образ dbmate) ---
# Приложение миграции не применяет: в compose это сервис migrate, локально — эти
# цели. Они запускают тот же сервис через compose, поэтому нужен живой Docker.
# Альтернатива без Docker: `brew install dbmate` и
#   dbmate --no-dump-schema -d backend/migrations \
#     -u "mysql://marketing:пароль@127.0.0.1:3306/marketing" up

## db-up: поднять MariaDB (нужен Docker; нужна для make dev и тестов с БД)
db-up:
	$(COMPOSE) up -d --wait mariadb

## db-down: остановить MariaDB
db-down:
	$(COMPOSE) stop mariadb

## migrate: применить миграции схемы (сервис migrate из compose)
migrate:
	$(COMPOSE) run --rm migrate

## migrate-down: откатить последнюю миграцию
migrate-down:
	$(COMPOSE) run --rm -e DBMATE_CMD=rollback migrate

# --- локальный запуск (без Docker) ---

## backend: API на 127.0.0.1:8080 (окружение читается из backend/.env)
backend:
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
