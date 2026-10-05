package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/run"
	"github.com/977ADAM/marketing-agents/internal/sqlite"
)

// testStores — три хранилища поверх одной БД: адаптер разделён по сущностям,
// поэтому и в тестах у каждой свой вход.
type testStores struct {
	campaigns *sqlite.Campaigns
	reviews   *sqlite.Reviews
	events    *sqlite.Events
	db        *sql.DB
}

// openStores открывает БД по пути и готовит схему (в приложении это делает
// отдельный сервис migrate).
func openStores(t *testing.T, path string) *testStores {
	t.Helper()
	db, err := sqlite.OpenDB(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	applyMigrations(t, db)
	return &testStores{
		campaigns: sqlite.NewCampaigns(db),
		reviews:   sqlite.NewReviews(db),
		events:    sqlite.NewEvents(db),
		db:        db,
	}
}

// newTestStore открывает отдельную SQLite-БД в t.TempDir(): тесты изолированы
// и не требуют внешнего сервера.
func newTestStore(t *testing.T) *testStores {
	t.Helper()
	return openStores(t, filepath.Join(t.TempDir(), "test.db"))
}

func TestRecoverInterrupted(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Сбросить возможные «осиротевшие» от прошлых тестов, чтобы count был детерминирован.
	if _, err := sqlite.RecoverInterrupted(ctx, st.db); err != nil {
		t.Fatalf("pre-drain: %v", err)
	}

	pendingID, err := st.campaigns.Create(ctx, "", campaign.Brief{})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	runningID, err := st.campaigns.Create(ctx, "", campaign.Brief{})
	if err != nil {
		t.Fatalf("create running: %v", err)
	}
	if err := st.campaigns.MarkRunning(ctx, runningID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	doneID, err := st.campaigns.Create(ctx, "", campaign.Brief{})
	if err != nil {
		t.Fatalf("create done: %v", err)
	}
	if err := st.campaigns.Complete(ctx, doneID, campaign.Outcome{}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	n, err := sqlite.RecoverInterrupted(ctx, st.db)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered count = %d, want 2", n)
	}

	for _, id := range []string{pendingID, runningID} {
		c, err := st.campaigns.Get(ctx, id)
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

	done, err := st.campaigns.Get(ctx, doneID)
	if err != nil {
		t.Fatalf("get done: %v", err)
	}
	if done.Status != "done" {
		t.Errorf("done campaign status = %q, want done (не тронута)", done.Status)
	}

	again, err := sqlite.RecoverInterrupted(ctx, st.db)
	if err != nil {
		t.Fatalf("recover again: %v", err)
	}
	if again != 0 {
		t.Errorf("second recover count = %d, want 0", again)
	}
}

// DSN включает foreign_keys=1: деливерабл с несуществующей кампанией не вставится.
func TestForeignKeysEnforced(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.db.ExecContext(ctx,
		`INSERT INTO deliverables (id, campaign_id, topic, title, body, cta, review)
		 VALUES ('d-1','00000000-0000-0000-0000-00000000dead','t','a','b','c','{}')`); err == nil {
		t.Fatal("ожидали ошибку FOREIGN KEY: pragma foreign_keys=1 не применилась")
	}
}

// DSN: нужные pragma на месте, готовый URI не переписывается.
func TestDSN(t *testing.T) {
	dsn := sqlite.DSN("data/x.db")
	for _, want := range []string{"file:data/x.db?", "busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)", "_txlock=immediate"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN = %q, нет %q", dsn, want)
		}
	}
	custom := "file:/tmp/x.db?_pragma=foreign_keys(1)"
	if got := sqlite.DSN(custom); got != custom {
		t.Errorf("DSN(готовый URI) = %q, want %q", got, custom)
	}
	if dsn := sqlite.DSN(":memory:"); dsn == "" || strings.Contains(dsn, "journal_mode") {
		t.Errorf("DSN(:memory:) = %q — WAL для памяти не нужен", dsn)
	}
}

// Прогресс пишется из параллельных горутин (по теме на горутину) — проверяем,
// что WAL + busy_timeout + _txlock=immediate не дают «database is locked».
func TestConcurrentProgressWrites(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.campaigns.Create(ctx, "", campaign.Brief{Product: "P"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const writers = 24
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			snap := run.Snapshot{
				Phase:      run.PhaseProducing,
				TopicTotal: 5,
				TopicsDone: n % 5,
				Percent:    n,
				Topics:     []run.TopicProgress{{Index: 0, Title: "T", State: run.TopicWriting, Iter: n}},
			}
			if err := st.campaigns.SaveProgress(ctx, id, snap); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("SaveProgress: %v", err)
	}

	got, err := st.campaigns.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Progress == nil {
		t.Fatal("Progress is nil after concurrent writes")
	}
}
