package migrations

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestMatchesSQL(t *testing.T) {
	files, err := filepath.Glob("*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(RequiredVersions) {
		t.Fatalf("SQL files=%d manifest=%d", len(files), len(RequiredVersions))
	}
	for i, file := range files {
		if strings.SplitN(file, "_", 2)[0] != RequiredVersions[i] {
			t.Fatalf("missing %s", file)
		}
	}
}
