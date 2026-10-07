// Package skills — встроенные копии скилов пакета marketing-skills: текст
// SKILL.md доступен сервису без чтения файловой системы во время работы.
// Копии обновляет цель make sync-skills.
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed prompts/*/SKILL.md
var promptFS embed.FS

// Names возвращает имена встроенных скилов в алфавитном порядке.
func Names() []string {
	paths, err := fs.Glob(promptFS, "prompts/*/SKILL.md")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, path.Base(path.Dir(p)))
	}
	sort.Strings(names)
	return names
}

// Prompt возвращает полный текст SKILL.md, включая frontmatter.
// Неизвестное имя или пустой текст — ошибка.
func Prompt(name string) (string, error) {
	paths, err := fs.Glob(promptFS, "prompts/*/SKILL.md")
	if err == nil {
		for _, p := range paths {
			if path.Base(path.Dir(p)) != name {
				continue
			}
			data, err := fs.ReadFile(promptFS, p)
			if err != nil {
				return "", fmt.Errorf("skills: не удалось прочитать скил %q: %w", name, err)
			}
			if strings.TrimSpace(string(data)) == "" {
				return "", fmt.Errorf("skills: скил %q пуст", name)
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("skills: неизвестный скил %q (доступны: %s)", name, strings.Join(Names(), ", "))
}
