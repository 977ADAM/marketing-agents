# Восстановление осиротевших кампаний при старте — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** При старте сервиса помечать осиротевшие после рестарта кампании (`pending`/`running`) как `failed`, чтобы они не висели вечно.

**Architecture:** Новый метод стора `RecoverInterrupted` делает один атомарный `UPDATE`; `cmd/server/main.go` вызывает его один раз на старте (после миграций, до приёма трафика).

**Tech Stack:** Go, pgx/v5 (`pgxpool`), интеграционные тесты под `//go:build integration` с реальным Postgres.

---

## Контекст и инварианты (прочитать перед стартом)

- Рабочая директория: `/home/adam/repos/marketing-agents`, ветка `feat/recover-interrupted`.
- Обычные тесты: `go test ./...` (без БД, должны оставаться зелёными).
- Интеграционные тесты стора: `go test -tags=integration ./internal/store` — требуют `DATABASE_URL`
  и поднятый Postgres. Харнес `newTestStore(t)` (в `internal/store/store_test.go`) сам мигрирует схему;
  если `DATABASE_URL` пуст — тест `t.Skip`.
- Сигнатуры стора (`internal/store/store.go`), которые используем в тесте:
  - `Create(ctx, clientID string, b agents.Brief) (string, error)` — создаёт кампанию в `pending`.
  - `MarkRunning(ctx, id string) error` — переводит в `running`.
  - `Complete(ctx, id string, res orchestrator.Result) error` — переводит в `done`.
  - `Get(ctx, id string) (*Campaign, error)` — `Campaign.Status`, `Campaign.Error`.
- `pool.Exec` возвращает `(pgconn.CommandTag, error)`; `tag.RowsAffected()` → `int64`.

### Поднять Postgres для интеграционных тестов (один раз перед Task 1)

```bash
docker rm -f ma-test-pg 2>/dev/null
docker run -d --name ma-test-pg -e POSTGRES_USER=app -e POSTGRES_PASSWORD=app \
  -e POSTGRES_DB=marketing -p 5433:5432 postgres:16
# дождаться готовности
for i in $(seq 1 20); do docker exec ma-test-pg pg_isready -U app -d marketing >/dev/null 2>&1 && break; sleep 1; done
export DATABASE_URL="postgres://app:app@localhost:5433/marketing?sslmode=disable"
```

(Порт 5433, чтобы не конфликтовать с возможным локальным Postgres на 5432.)

## Структура файлов

- Modify: `internal/store/store.go` — добавить метод `RecoverInterrupted`.
- Modify: `internal/store/store_test.go` — добавить интеграционный тест (под существующим build-тегом).
- Modify: `cmd/server/main.go` — вызвать `RecoverInterrupted` на старте.

---

### Task 1: Метод `Store.RecoverInterrupted` + интеграционный тест

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Написать падающий тест**

Добавить в конец `internal/store/store_test.go`:

```go
func TestRecoverInterrupted(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Сбросить возможные «осиротевшие» от прошлых тестов, чтобы count был детерминирован.
	if _, err := st.RecoverInterrupted(ctx); err != nil {
		t.Fatalf("pre-drain: %v", err)
	}

	// pending
	pendingID, err := st.Create(ctx, "", agents.Brief{})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	// running
	runningID, err := st.Create(ctx, "", agents.Brief{})
	if err != nil {
		t.Fatalf("create running: %v", err)
	}
	if err := st.MarkRunning(ctx, runningID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	// done
	doneID, err := st.Create(ctx, "", agents.Brief{})
	if err != nil {
		t.Fatalf("create done: %v", err)
	}
	if err := st.Complete(ctx, doneID, orchestrator.Result{}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	n, err := st.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered count = %d, want 2", n)
	}

	for _, id := range []string{pendingID, runningID} {
		c, err := st.Get(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if c.Status != "failed" {
			t.Errorf("campaign %s status = %q, want failed", id, c.Status)
		}
		if c.Error != "прервано рестартом сервиса" {
			t.Errorf("campaign %s error = %q, want «прервано рестартом сервиса»", id, c.Error)
		}
	}

	done, err := st.Get(ctx, doneID)
	if err != nil {
		t.Fatalf("get done: %v", err)
	}
	if done.Status != "done" {
		t.Errorf("done campaign status = %q, want done (не тронута)", done.Status)
	}

	// идемпотентность
	again, err := st.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatalf("recover again: %v", err)
	}
	if again != 0 {
		t.Errorf("second recover count = %d, want 0", again)
	}
}
```

- [ ] **Step 2: Запустить — убедиться, что не компилируется/падает**

Run: `go test -tags=integration ./internal/store -run TestRecoverInterrupted` (с заданным `DATABASE_URL`)
Expected: ошибка компиляции — `st.RecoverInterrupted undefined`.

- [ ] **Step 3: Реализовать метод**

Добавить в `internal/store/store.go` (например, после метода `Fail`):

```go
// RecoverInterrupted помечает осиротевшие после рестарта кампании (pending/running)
// как failed. Возвращает число восстановленных. Идемпотентен.
func (s *Store) RecoverInterrupted(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status='failed', error='прервано рестартом сервиса', updated_at=now()
		 WHERE status IN ('pending','running')`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 4: Запустить — тест зелёный**

Run: `go test -tags=integration ./internal/store -run TestRecoverInterrupted -v`
Expected: PASS.

- [ ] **Step 5: Обычные тесты не сломаны**

Run: `go test ./...`
Expected: всё PASS (без build-тега `integration` стор-тесты пропускаются).

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): RecoverInterrupted — пометить осиротевшие кампании failed

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Вызов `RecoverInterrupted` на старте сервиса

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Вставить вызов на старте**

В `cmd/server/main.go` сразу после строки `st := store.New(pool)` добавить:

```go
	if n, err := st.RecoverInterrupted(baseCtx); err != nil {
		logger.Error("recover interrupted", "err", err)
		os.Exit(1)
	} else if n > 0 {
		logger.Info("recovered interrupted campaigns", "count", n)
	}
```

(`os.Exit`, `logger` уже используются выше в файле — новых импортов не требуется.)

- [ ] **Step 2: Сборка и vet**

Run: `go build ./... && go vet ./...`
Expected: без ошибок.

- [ ] **Step 3: Полный прогон обычных тестов**

Run: `go test ./...`
Expected: всё PASS.

- [ ] **Step 4: Интеграционный прогон стора (с поднятым Postgres)**

Run: `go test -tags=integration ./internal/store`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): восстанавливать осиротевшие кампании при старте

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Прибрать тестовый Postgres

- [ ] **Step 1: Удалить контейнер**

```bash
docker rm -f ma-test-pg
```

Expected: контейнер удалён. (Если используешь общий dev-Postgres, а не одноразовый контейнер — пропусти.)

---

## Self-review (выполнено при написании плана)

- **Покрытие спеки:** метод `RecoverInterrupted` (Task 1), интеграционный тест с pending+running→failed,
  done не тронут, идемпотентность (Task 1, Step 1), вызов на старте + фатальная ошибка (Task 2). Все
  разделы спеки покрыты.
- **Плейсхолдеров нет:** весь код и команды приведены целиком.
- **Согласованность сигнатур:** `RecoverInterrupted(ctx) (int64, error)` одинаково в тесте, реализации
  и вызове из `main`. Сообщение ошибки «прервано рестартом сервиса» совпадает в реализации и в проверке теста.
