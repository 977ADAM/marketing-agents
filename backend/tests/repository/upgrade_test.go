package repository_test

import (
	"context"
	schema "github.com/977ADAM/marketing-agents/internal/core/repository/mariadb"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	traceservice "github.com/977ADAM/marketing-agents/internal/features/trace/service"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestUpgradeLegacyAndRollbackTemporaryDatabase(t *testing.T) {
	s := newEmptyStore(t)
	ctx := context.Background()
	files, e := filepath.Glob("../../migrations/*.sql")
	if e != nil || len(files) < 8 {
		t.Fatalf("migrations %v %v", files, e)
	}
	sort.Strings(files)
	if e = s.db.Exec("CREATE TABLE schema_migrations (version varchar(128) PRIMARY KEY)").Error; e != nil {
		t.Fatal(e)
	}
	apply := func(file string, up bool) {
		t.Helper()
		body, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		sections := strings.Split(string(body), "-- migrate:up")
		parts := strings.Split(sections[1], "-- migrate:down")
		sql := parts[0]
		if !up {
			sql = parts[1]
		}
		for _, stmt := range strings.Split(sql, ";") {
			if strings.TrimSpace(stmt) != "" {
				if e = s.db.Exec(stmt).Error; e != nil {
					t.Fatalf("%s: %v", file, e)
				}
			}
		}
		version := filepath.Base(file)[:4]
		if up {
			e = s.db.Exec("INSERT INTO schema_migrations(version) VALUES (?)", version).Error
		} else {
			e = s.db.Exec("DELETE FROM schema_migrations WHERE version=?", version).Error
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, f := range files[:2] {
		apply(f, true)
	}
	if e = s.db.Exec(`INSERT INTO campaigns(id,client_id,status,brief,strategy) VALUES ('legacy-c','00000000-0000-0000-0000-000000000001','done','{"product":"old"}','{"positioning":"old","topics":[]}')`).Error; e != nil {
		t.Fatal(e)
	}
	if e = s.db.Exec(`INSERT INTO reviews(id,client_id,status,brief_text,error) VALUES ('legacy-r','00000000-0000-0000-0000-000000000001','failed','old brief','old error')`).Error; e != nil {
		t.Fatal(e)
	}
	for _, f := range files[2:] {
		apply(f, true)
	}
	if _, e = schema.CheckSchema(ctx, s.db); e != nil {
		t.Fatal(e)
	}
	c, e := s.campaigns.Get(ctx, "legacy-c")
	if e != nil || c.Brief.Product != "old" || c.Status != "done" {
		t.Fatalf("legacy campaign %+v %v", c, e)
	}
	r, e := s.reviews.GetCheck(ctx, "legacy-r")
	if e != nil || r.Error != "old error" || r.ResumeAvailable {
		t.Fatalf("legacy review %+v %v", r, e)
	}
	rec := traceservice.New(s.events, trace.Config{Mode: trace.ModeSummary})
	rec.Event(trace.WithRunID(ctx, "legacy-c"), trace.Event{Kind: trace.KindResult, Name: "test"})
	for i := len(files) - 1; i >= 2; i-- {
		apply(files[i], false)
	}
	for _, f := range files[2:] {
		apply(f, true)
	}
	if _, e = schema.CheckSchema(ctx, s.db); e != nil {
		t.Fatal(e)
	}
	if _, e = s.campaigns.Get(ctx, "legacy-c"); e != nil {
		t.Fatal(e)
	}
}
