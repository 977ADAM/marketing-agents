package skills_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/adapters/skills"
)

// packageSkillsDir возвращает путь к каноническому каталогу скилов
// (<repo>/marketing-skills/skills). Корень репозитория вычисляется от
// расположения этого файла: поднимаемся до каталога с go.mod, затем на
// уровень выше. Если каталога нет (backend собран отдельно от репозитория) —
// тест пропускается, а не падает.
func packageSkillsDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к тесту через runtime.Caller")
	}

	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("не найден go.mod выше %s", filepath.Dir(file))
		}
		dir = parent
	}
	// dir — каталог backend с go.mod, корень репозитория на уровень выше.
	src := filepath.Join(filepath.Dir(dir), "marketing-skills", "skills")

	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		t.Skip("пакет marketing-skills не найден: backend собран отдельно от репозитория; запустите make sync-skills в репозитории")
	}
	return src
}

// sortedSkillNames возвращает отсортированную копию имён скилов.
func sortedSkillNames(names []string) []string {
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	return sorted
}

// TestEmbeddedCopiesMatchPackage проверяет, что встроенные копии скилов
// побайтово совпадают с каноническим пакетом marketing-skills и что наборы
// имён совпадают в обе стороны. Дрейф лечится целью make sync-skills.
func TestEmbeddedCopiesMatchPackage(t *testing.T) {
	src := packageSkillsDir(t)

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("не удалось прочитать каталог пакета скилов %s: %v", src, err)
	}

	embeddedNames := skills.Names()
	var fromPackage []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fromPackage = append(fromPackage, e.Name())

		raw, err := os.ReadFile(filepath.Join(src, e.Name(), "SKILL.md"))
		if err != nil {
			t.Fatalf("не удалось прочитать скил %s пакета: %v; запустите make sync-skills", e.Name(), err)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			t.Fatalf("копия %s пуста: запустите make sync-skills", e.Name())
		}

		// Отсутствие скила во встроенных копиях сообщит сравнение наборов
		// ниже; здесь сравниваем байты только для тех, что встроены.
		if !slices.Contains(embeddedNames, e.Name()) {
			continue
		}
		embedded, err := skills.Prompt(e.Name())
		if err != nil {
			t.Errorf("не удалось получить встроенную копию скила %s: %v; запустите make sync-skills", e.Name(), err)
			continue
		}
		if embedded != string(raw) {
			t.Errorf("копия скила %s разошлась с пакетом: запустите make sync-skills", e.Name())
		}
	}

	fromPackage = sortedSkillNames(fromPackage)
	embeddedNames = sortedSkillNames(embeddedNames)

	if !slices.Equal(fromPackage, embeddedNames) {
		t.Errorf("набор скилов в пакете %v и во встроенных копиях %v различаются: запустите make sync-skills",
			fromPackage, embeddedNames)
	}
	// Проверяем расхождение в обе стороны отдельно, чтобы в отчёте было видно,
	// какой именно скил потерян или лишний.
	for _, name := range fromPackage {
		if !slices.Contains(embeddedNames, name) {
			t.Errorf("скил %s есть в пакете, но отсутствует во встроенных копиях: запустите make sync-skills", name)
		}
	}
	for _, name := range embeddedNames {
		if !slices.Contains(fromPackage, name) {
			t.Errorf("скил %s встроен, но отсутствует в пакете: запустите make sync-skills", name)
		}
	}
}
