package skills_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/adapters/skills"
)

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
	want := []string{"article-review", "campaign-context", "campaign-plan", "native-article"}
	if got := skills.Names(); !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}
