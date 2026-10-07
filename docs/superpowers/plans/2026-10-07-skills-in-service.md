# Скилы пакета marketing-skills в промптах агентов — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development (recommended) or executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Собирать системные промпты девяти ролей сервиса из файлов пакета `marketing-skills`, не меняя JSON-контракты агентов.

**Architecture:** Новый адаптер `backend/internal/adapters/skills` — декоратор над `corellm.Client`: по роли он подставляет `тело SKILL.md + оговорка + исходный system`. Копии скилов встроены через `go:embed`, синхронизируются целью `make sync-skills` и защищены тестом на дрейф. Карта «роль → скил» собирается в composition root (`cmd/server`), поэтому адаптер не зависит от `features`.

**Tech Stack:** Go 1.25, `go:embed`, стандартный `testing`; GNU Make. Новых зависимостей нет.

**Spec:** `docs/superpowers/specs/2026-10-07-skills-in-service-design.md`

## Global Constraints

- **Не меняется:** JSON-контракты агентов (`{"positioning","topics"}`, `{"topic","title","body","cta"}`, `{"score","issues","verdict"}`, `{"score","issues"}`), домен, схема БД и миграции, API, фронт, число вызовов LLM на прогон, валидация ответов, учёт токенов и цен.
- **Порядок декораторов:** `accounting.New(skills.New(tracing.NewLLM(baseLLM, recorder)))` — скилы снаружи трассы, иначе в трассе останется промпт без скила.
- **Карта ролей (9):** `strategist`, `semanticist_seeds`, `semanticist_cluster`, `semanticist_select`, `semanticist_fallback` → `campaign-plan`; `copywriter` → `native-article`; `critic`, `compliance`, `quality` → `article-review`. `campaign-context` не применяется.
- **Копии скилов:** `backend/internal/adapters/skills/prompts/<name>/SKILL.md`, коммитятся в git, правятся только через `make sync-skills` (источник — `marketing-skills/skills/`).
- **Зависимости:** пакет `internal/adapters/skills` не импортирует `internal/features/...`; карта ролей собирается в `cmd/server`.
- **Новых переменных окружения нет**, скилы всегда включены; откат — откат коммита.
- **Стиль проекта:** юнит-тесты — black-box (`package skills_test`, `package main`), комментарии и пользовательские тексты по-русски, ошибки возвращаются значениями.
- **Версия пакета после работы:** `metadata.version: 0.2.0` во всех четырёх `SKILL.md`, README пакета — «v0.2».

## Review Focus

Классы входов и отказов, которые спецификация подразумевает, а тесты задач обязаны закрыть:

1. **Новая роль, которой ещё нет в карте** — прогон не падает, промпт уходит без скила (тест в Task 2).
2. **Обрезанная или пустая копия скила** — тест на дрейф падает с указанием `make sync-skills`, а не отправляет модели пустой префикс (Task 3).
3. **Скил в промпте ровно один раз** — один вызов не должен добавлять текст скила дважды (Task 2).
4. **Ошибка модели** — до агента доходят и ошибка, и `usage`, а промпт всё равно составлен: трасса показывает, что ушло (Task 2).
5. **Пакет отсутствует** (backend собран отдельно от репозитория) — тест на дрейф скипается с объяснением, а не падает необъяснимо (Task 3).

## File Structure

- `backend/internal/adapters/skills/prompts.go` — `go:embed` и доступ к тексту скила (`Names`, `Prompt`).
- `backend/internal/adapters/skills/skills.go` — декоратор, `Options`, оговорка `contractCaveat`.
- `backend/internal/adapters/skills/prompts/{campaign-plan,native-article,article-review}/SKILL.md` — синхронизированные копии.
- `backend/internal/adapters/skills/{prompts_test.go,skills_test.go,drift_test.go}` — юнит-тесты, тест дрейфа.
- `backend/cmd/server/skills.go` — карта «роль → скил» (`skillBindings`).
- `backend/cmd/server/skills_test.go` — исчерпывающий охват ролей и порядок декораторов.
- `backend/cmd/server/main.go` — подключение декоратора в цепочку.
- `Makefile` — цель `sync-skills`.
- `marketing-skills/skills/*/SKILL.md`, `marketing-skills/README.md`, `README.md` — версия и документация.

---

### Task 1: Встроенные копии скилов и доступ к ним

**Files:**
- Modify: `Makefile` (секция подготовки окружения, блок `.PHONY` в строках 10–14)
- Create: `backend/internal/adapters/skills/prompts.go`
- Create: `backend/internal/adapters/skills/prompts/{campaign-plan,native-article,article-review}/SKILL.md` (создаёт `make sync-skills`)
- Test: `backend/internal/adapters/skills/prompts_test.go`

**Interfaces:**
- Consumes: ничего.
- Produces: `func Names() []string` — имена встроенных скилов, отсортированные; `func Prompt(name string) (string, error)` — полный текст `SKILL.md`, включая frontmatter; ошибка на неизвестное имя и на пустое содержимое.

- [ ] **Step 1: Добавить цель `sync-skills` в Makefile**

В блок `.PHONY` (строки 10–14) добавить `sync-skills`. В секцию подготовки окружения после цели `deps-backend` добавить:

```make
## sync-skills: скопировать скилы пакета marketing-skills во встроенные копии бэкенда
sync-skills:
	@for d in marketing-skills/skills/*/; do \
		name=$$(basename $$d); \
		mkdir -p $(BACKEND)/internal/adapters/skills/prompts/$$name; \
		cp $$d/SKILL.md $(BACKEND)/internal/adapters/skills/prompts/$$name/SKILL.md; \
	done
	@echo "скилы синхронизированы: $(BACKEND)/internal/adapters/skills/prompts"
```

- [ ] **Step 2: Синхронизировать копии**

Run: `make sync-skills && find backend/internal/adapters/skills/prompts -type f | sort`
Expected: три файла — `prompts/article-review/SKILL.md`, `prompts/campaign-plan/SKILL.md`, `prompts/native-article/SKILL.md`.

- [ ] **Step 3: Написать падающий тест**

`backend/internal/adapters/skills/prompts_test.go`, `package skills_test`:

```go
func TestPromptReturnsEmbeddedSkillVerbatim(t *testing.T) {
	got, err := skills.Prompt("native-article")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !strings.HasPrefix(got, "---") {
		t.Errorf("frontmatter потерян: %q", got[:20])
	}
	if !strings.Contains(got, "name: native-article") {
		t.Error("в тексте нет имени скила из frontmatter")
	}
	if strings.TrimSpace(got) == "" {
		t.Error("скил пуст")
	}
}

func TestPromptRejectsUnknownName(t *testing.T) {
	if _, err := skills.Prompt("нет-такого"); err == nil {
		t.Fatal("ожидалась ошибка на неизвестное имя")
	}
}

func TestNamesListsAllEmbeddedSkills(t *testing.T) {
	want := []string{"article-review", "campaign-plan", "native-article"}
	if got := skills.Names(); !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 4: Убедиться, что тест падает**

Run: `cd backend && go test ./internal/adapters/skills/ -run 'TestPrompt|TestNames' -v`
Expected: FAIL — `undefined: skills.Prompt`.

- [ ] **Step 5: Реализовать `prompts.go`**

```go
//go:embed prompts/*/SKILL.md
var promptFS embed.FS

// Names возвращает имена встроенных скилов в алфавитном порядке.
func Names() []string

// Prompt возвращает полный текст SKILL.md, включая frontmatter.
// Неизвестное имя или пустой текст — ошибка.
func Prompt(name string) (string, error)
```

Реализация: пути берутся через `fs.Glob(promptFS, "prompts/*/SKILL.md")`, имя — `path.Base(path.Dir(p))`; текст — `fs.ReadFile`. Ошибка неизвестного имени перечисляет доступные: `fmt.Errorf("skills: неизвестный скил %q (доступны: %s)", name, strings.Join(Names(), ", "))`.

- [ ] **Step 6: Убедиться, что тесты проходят**

Run: `cd backend && go test ./internal/adapters/skills/ -v`
Expected: PASS.

- [ ] **Step 7: Коммит**

```bash
git add Makefile backend/internal/adapters/skills
git commit -m "feat(skills): встроить копии скилов пакета и цель sync-skills"
```

---

### Task 2: Декоратор, собирающий промпт из скила

**Files:**
- Create: `backend/internal/adapters/skills/skills.go`
- Test: `backend/internal/adapters/skills/skills_test.go`

**Interfaces:**
- Consumes: `skills.Prompt(name string) (string, error)` (Task 1).
- Produces: `type Options struct { Bindings map[string]string }`; `func New(inner corellm.Client, opts Options) (*Client, error)`; `func (*Client) Complete(ctx context.Context, role, system, user string, out any) (corellm.Usage, error)`.

- [ ] **Step 1: Написать падающие тесты**

`backend/internal/adapters/skills/skills_test.go`, `package skills_test`. Фейк внутреннего клиента:

```go
type fakeClient struct {
	gotRole, gotSystem, gotUser string
	gotOut                      any
	usage                       corellm.Usage
	err                         error
}

func (f *fakeClient) Complete(_ context.Context, role, system, user string, out any) (corellm.Usage, error) {
	f.gotRole, f.gotSystem, f.gotUser, f.gotOut = role, system, user, out
	return f.usage, f.err
}
```

Тесты:

```go
const contract = "Ответ строго в JSON: {\"topic\": \"...\"}"

func TestCompleteComposesSkillBeforeContract(t *testing.T) {
	fake := &fakeClient{}
	c, err := skills.New(fake, skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	// ...
	_, err = c.Complete(context.Background(), "copywriter", contract, "бриф", &struct{}{})
	skill, _ := skills.Prompt("native-article")
	if !strings.HasPrefix(fake.gotSystem, skill) {
		t.Error("промпт не начинается с текста скила")
	}
	if !strings.Contains(fake.gotSystem, "Формат ответа этого сервиса важнее инструкций выше") {
		t.Error("нет оговорки о приоритете формата")
	}
	if !strings.HasSuffix(fake.gotSystem, contract) {
		t.Error("контракт роли должен быть последним")
	}
}

func TestCompletePassesThroughRoleWithoutBinding(t *testing.T)      // gotSystem == contract, gotUser == user
func TestCompleteAddsSkillExactlyOnce(t *testing.T)                // strings.Count(gotSystem, skill) == 1
func TestNewRejectsUnknownSkillName(t *testing.T)                  // New(..., "нет-такого") → err != nil, текст содержит имя
func TestCompletePropagatesUsageErrorAndOut(t *testing.T)          // usage равен, errors.Is(err, sentinel), fake.gotOut == any(&out), промпт составлен
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `cd backend && go test ./internal/adapters/skills/ -run TestComplete -v`
Expected: FAIL — `undefined: skills.New`.

- [ ] **Step 3: Реализовать `skills.go`**

```go
type Options struct{ Bindings map[string]string }

type Client struct {
	inner   corellm.Client
	prompts map[string]string // роль → текст скила
}

func New(inner corellm.Client, opts Options) (*Client, error)
func (c *Client) Complete(ctx context.Context, role, system, user string, out any) (corellm.Usage, error)
```

`New` для каждой роли вызывает `Prompt(skill)` и при ошибке оборачивает её ролью: `fmt.Errorf("skills: роль %q: %w", role, err)`. Роль без байнда — passthrough. `Complete` при наличии текста собирает `system = text + "\n\n" + contractCaveat + "\n\n" + system`; `usage`, `out` и ошибка возвращаются как есть.

Оговорка — точный текст (константа `contractCaveat` в этом же файле):

```go
const contractCaveat = `Формат ответа этого сервиса важнее инструкций выше.
Ответ — только JSON по схеме, без markdown-обёрток, файлов и текста вне JSON.
Файлы не создаются: раздел «Результат: …» выше описывает требуемое содержание
и структуру, а не файл на диске. Служебные разделы скила (метаданные, реестры
утверждений и замечаний, недостающие данные) в ответ не выносятся и новых ключей
JSON не добавляют. Там, где скил требует замечание с важностью, цитатой,
нарушенным критерием, требуемой правкой и критерием закрытия, всё это помещается
в строку соответствующего поля JSON. Если скил предлагает значение, которого нет
в схеме (например verdict "needs_input"), используй ближайшее допустимое
("revise"). Оценка и вердикт проверяются кодом: отсутствующее или недопустимое
значение — ошибка.`
```

- [ ] **Step 4: Убедиться, что тесты проходят**

Run: `cd backend && go test ./internal/adapters/skills/ -v`
Expected: PASS (все тесты Task 1 и Task 2).

- [ ] **Step 5: Коммит**

```bash
git add backend/internal/adapters/skills
git commit -m "feat(skills): декоратор LLM-клиента, подставляющий скил по роли"
```

---

### Task 3: Тест на дрейф между копиями и пакетом

**Files:**
- Test: `backend/internal/adapters/skills/drift_test.go`

**Interfaces:**
- Consumes: `skills.Names()`, `skills.Prompt(name)` (Task 1); цель `make sync-skills` (Task 1).
- Produces: ничего.

- [ ] **Step 1: Написать тест**

`drift_test.go`, `package skills_test`:

```go
func TestEmbeddedCopiesMatchPackage(t *testing.T) {
	src := packageSkillsDir(t) // <repo>/marketing-skills/skills; t.Skip, если каталога нет
	entries, err := os.ReadDir(src)
	// ...
	var fromPackage []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fromPackage = append(fromPackage, e.Name())
		raw, err := os.ReadFile(filepath.Join(src, e.Name(), "SKILL.md"))
		// ...
		if len(bytes.TrimSpace(raw)) == 0 {
			t.Fatalf("копия %s пуста: запустите make sync-skills", e.Name())
		}
		embedded, err := skills.Prompt(e.Name())
		// ...
		if embedded != string(raw) {
			t.Errorf("копия скила %s разошлась с пакетом: запустите make sync-skills", e.Name())
		}
	}
	if !slices.Equal(sorted(fromPackage), skills.Names()) {
		t.Errorf("набор скилов в пакете %v и во встроенных копиях %v различаются: запустите make sync-skills",
			sorted(fromPackage), skills.Names())
	}
}
```

Хелпер `packageSkillsDir(t)`: от `runtime.Caller(0)` подниматься до каталога с `go.mod`, затем на уровень выше — это корень репозитория; вернуть `filepath.Join(root, "marketing-skills", "skills")`. Если каталога нет — `t.Skip("пакет marketing-skills не найден: backend собран отдельно от репозитория; запустите make sync-skills в репозитории")`.

- [ ] **Step 2: Убедиться, что тест проходит на синхронных копиях**

Run: `cd backend && go test ./internal/adapters/skills/ -run TestEmbeddedCopiesMatchPackage -v`
Expected: PASS.

- [ ] **Step 3: Убедиться, что тест ловит дрейф и новый скил**

Run:
```bash
printf '\n<!-- drift -->\n' >> backend/internal/adapters/skills/prompts/native-article/SKILL.md
cd backend && go test ./internal/adapters/skills/ -run TestEmbeddedCopiesMatchPackage
mkdir -p ../marketing-skills/skills/zz-tmp && printf -- '---\nname: zz-tmp\n---\n' > ../marketing-skills/skills/zz-tmp/SKILL.md
go test ./internal/adapters/skills/ -run TestEmbeddedCopiesMatchPackage
```
Expected: первое падение с текстом про `make sync-skills`; второе — падение на расхождении набора скилов.

- [ ] **Step 4: Вернуть синхронное состояние**

Run: `make sync-skills && rm -rf marketing-skills/skills/zz-tmp && cd backend && go test ./internal/adapters/skills/ -v`
Expected: PASS, лишний каталог удалён.

- [ ] **Step 5: Коммит**

```bash
git add backend/internal/adapters/skills/drift_test.go
git commit -m "test(skills): проверять, что встроенные копии не разошлись с пакетом"
```

---

### Task 4: Карта ролей и подключение декоратора

**Files:**
- Create: `backend/cmd/server/skills.go`
- Create: `backend/cmd/server/skills_test.go`
- Modify: `backend/cmd/server/main.go:126-129`

**Interfaces:**
- Consumes: `skills.New`, `skills.Options` (Task 2); константы `campaignservice.RoleStrategist/RoleCopywriter/RoleCritic`, `reviewservice.RoleCompliance/RoleQuality`, `topicservice.RoleSeeds/RoleCluster/RoleSelect/RoleFallback`.
- Produces: `func skillBindings() map[string]string` в `package main`.

- [ ] **Step 1: Написать падающий тест**

`backend/cmd/server/skills_test.go`, `package main`:

```go
func TestSkillBindingsCoverAllRoles(t *testing.T) {
	want := map[string]string{
		campaignservice.RoleStrategist:     "campaign-plan",
		campaignservice.RoleCopywriter:     "native-article",
		campaignservice.RoleCritic:         "article-review",
		reviewservice.RoleCompliance:       "article-review",
		reviewservice.RoleQuality:          "article-review",
		topicservice.RoleSeeds:             "campaign-plan",
		topicservice.RoleCluster:           "campaign-plan",
		topicservice.RoleSelect:            "campaign-plan",
		topicservice.RoleFallback:          "campaign-plan",
	}
	got := skillBindings()
	if !maps.Equal(got, want) {
		t.Errorf("skillBindings() = %v, want %v", got, want)
	}
	if _, err := skills.New(&nopClient{}, skills.Options{Bindings: got}); err != nil {
		t.Fatalf("карта ролей ссылается на отсутствующий скил: %v", err)
	}
}

type nopClient struct{}

func (nopClient) Complete(context.Context, string, string, string, any) (corellm.Usage, error) {
	return corellm.Usage{}, nil
}
```

Порядок декораторов (в том же файле):

```go
type captureClient struct{ system string }

func (c *captureClient) Complete(_ context.Context, _, system, _ string, _ any) (corellm.Usage, error) {
	c.system = system
	return corellm.Usage{}, nil
}

type captureRecorder struct{ events []trace.Event }

func (r *captureRecorder) Event(_ context.Context, ev trace.Event) { r.events = append(r.events, ev) }

func TestSkillsWrapOutsideTracing(t *testing.T) {
	rec := &captureRecorder{}
	c, err := skills.New(tracing.NewLLM(&captureClient{}, rec),
		skills.Options{Bindings: map[string]string{"copywriter": "native-article"}})
	// ...
	_, err = c.Complete(context.Background(), "copywriter", "КОНТРАКТ", "бриф", &struct{}{})
	// ...
	payload, _ := rec.events[0].Payload["system"].(string)
	if !strings.Contains(payload, "name: native-article") || !strings.Contains(payload, "КОНТРАКТ") {
		t.Errorf("трасса записала промпт без скила: %q", payload)
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `cd backend && go test ./cmd/server/ -v`
Expected: FAIL — `undefined: skillBindings`.

- [ ] **Step 3: Реализовать карту и подключить декоратор**

`backend/cmd/server/skills.go`:

```go
// skillBindings связывает роли агентов со скилами пакета marketing-skills.
// Собирается здесь, а не в адаптере: адаптеры не зависят от фич.
func skillBindings() map[string]string
```

Возвращает карту ровно из девяти пар (значения — как в Step 1).

`backend/cmd/server/main.go`, строки 126–129 — заменить `llmClient := accounting.New(tracing.NewLLM(baseLLM, recorder))` на:

```go
skilled, err := skills.New(tracing.NewLLM(baseLLM, recorder), skills.Options{Bindings: skillBindings()})
if err != nil {
	logger.Error("skills", "err", err)
	os.Exit(1)
}
llmClient := accounting.New(skilled)
```

Добавить импорт `"github.com/977ADAM/marketing-agents/internal/adapters/skills"`.

- [ ] **Step 4: Убедиться, что тесты, тесты агентов и сборка проходят**

Run: `cd backend && go test ./cmd/server/ -v && go test ./internal/features/campaign/... ./internal/features/review/... ./internal/features/topic/... && go build ./...`
Expected: PASS, сборка без ошибок; тесты агентов не менялись и продолжают проверять контрактный слой.

- [ ] **Step 5: Коммит**

```bash
git add backend/cmd/server
git commit -m "feat(skills): карта ролей и сборка промптов из пакета в composition root"
```

---

### Task 5: Версия пакета и документация

**Files:**
- Modify: `marketing-skills/skills/campaign-context/SKILL.md`, `.../campaign-plan/SKILL.md`, `.../native-article/SKILL.md`, `.../article-review/SKILL.md`
- Modify: `marketing-skills/README.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: цель `make sync-skills` (Task 1), тест на дрейф (Task 3).
- Produces: ничего.

- [ ] **Step 1: Поднять версию скилов до 0.2.0**

Во всех четырёх `marketing-skills/skills/*/SKILL.md` заменить в frontmatter `version: 0.1.0` на `version: 0.2.0`.

- [ ] **Step 2: Синхронизировать копии**

Run: `make sync-skills && cd backend && go test ./internal/adapters/skills/ -run TestEmbeddedCopiesMatchPackage -v`
Expected: PASS; без синхронизации тест на дрейф падал бы.

- [ ] **Step 3: Обновить README пакета**

В `marketing-skills/README.md`: заголовок и вводный абзац — версия v0.2; добавить раздел «Использование в сервисе» с фактами: три скила (`campaign-plan`, `native-article`, `article-review`) подставляются в промпты агентов Go-сервиса через `backend/internal/adapters/skills`, копии синхронизируются целью `make sync-skills`, а `campaign-context` в сервисе пока не используется (нет агента нормализации брифа). Фразу «Интеграция с Go backend, изменение интерфейса и подключение MCP не входят в эту версию пакета» заменить на актуальную: интеграция с backend ограничена промптами, API/UI и MCP по-прежнему не входят.

- [ ] **Step 4: Обновить корневой README**

В `README.md`:
- в блоке структуры в строке `internal/adapters/` добавить `skills` (промпты ролей из пакета `marketing-skills`);
- в строке про `marketing-skills/` указать, что три скила применяются к промптам агентов;
- в разделе «Слои и устойчивость прогонов» добавить абзац: промпты агентов собираются адаптером `internal/adapters/skills` как `тело SKILL.md + оговорка + JSON-контракт роли`; копии синхронизируются `make sync-skills` и защищены тестом на дрейф; промпт растёт примерно на 900–1300 токенов на вызов, что видно в трассе и в стоимости прогона.

- [ ] **Step 5: Проверить и закоммитить**

Run: `cd backend && go test ./internal/adapters/skills/ ./cmd/server/ && cd .. && git diff --stat`
Expected: тесты проходят, в diff — четыре `SKILL.md` пакета, три копии, два README.

```bash
git add marketing-skills README.md backend/internal/adapters/skills/prompts
git commit -m "docs: версия пакета 0.2 и связь скилов с промптами сервиса"
```

---

### Task 6: Приёмка на живом прогоне

**Files:** изменений нет.

**Interfaces:**
- Consumes: всё предыдущее.
- Produces: ничего.

- [ ] **Step 1: Полная проверка репозитория**

Run: `make verify`
Expected: vet и svelte-check без замечаний, тесты Go (с MariaDB) и фронта зелёные, сборки бэкенда и фронта успешны.

- [ ] **Step 2: Поднять БД, применить схему и запустить API**

Run: `make db-up && make migrate`, затем в фоне `cd backend && go run ./cmd/server`
Expected: в логе `db ready`, `trace mode=full`; процесса нет в состоянии падения (иначе — ошибка карты скилов в логе).

- [ ] **Step 3: Живой прогон кампании**

Run:
```bash
curl -sS -XPOST localhost:8080/api/campaigns -H 'Content-Type: application/json' \
  -d '{"product":"Эко-бутылка","goal":"рост продаж","audience":"ЗОЖ 25-40","tone":"дружелюбный","topics_count":1}'
```
Дождаться статуса `done` через `GET /api/campaigns/{id}` (опрос раз в несколько секунд).
Expected: результат содержит стратегию и статью — то есть JSON-контракты ролей `strategist` и `copywriter` не сломаны скилами.

- [ ] **Step 4: Проверить, что скил дошёл до модели и виден в трассе**

Run:
```bash
curl -sS "localhost:8080/api/campaigns/{id}/trajectory?limit=200" | grep -o '"name":"copywriter"' | head -1
curl -sS "localhost:8080/api/campaigns/{id}/trajectory/{seq}" | grep -c 'name: native-article'
curl -sS "localhost:8080/api/campaigns/{id}/trajectory/{seq}" | grep -c 'Ты — копирайтер нативных статей'
```
где `seq` — первое событие с ролью копирайтера.
Expected: обе проверки дают не ноль — в payload события есть и текст скила (`name: native-article` из frontmatter), и JSON-контракт роли.

- [ ] **Step 5: Живой прогон проверки текста**

Run:
```bash
curl -sS -XPOST localhost:8080/api/reviews -H 'Content-Type: application/json' \
  -d '{"brief":"Продукт: бутылка. Запрет: не упоминать конкурентов.","texts":[{"title":"Тест","body":"Эко-бутылка держит тепло 6 часов и экономит 80% времени."}]}'
```
Expected: отчёт по каждому тексту с `compliance`/`quality` и `issues` — контракты `compliance` и `quality` целы; в трассе роль `quality` также содержит текст скила.

- [ ] **Step 6: Остановить API и БД**

Run: `make db-down`
Expected: фоновая команда `go run ./cmd/server` остановлена; том MariaDB сохранён.
