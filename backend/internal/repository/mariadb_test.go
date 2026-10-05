package mariadb_test

import (
	"context"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/977ADAM/marketing-agents/internal/core/repository/mariadb/pool"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	"github.com/977ADAM/marketing-agents/internal/repository"
	testdb "github.com/977ADAM/marketing-agents/internal/testkit/testdb"
)

// testStores — три хранилища поверх одной БД: адаптер разделён по сущностям,
// поэтому и в тестах у каждой свой вход.
type testStores struct {
	campaigns *mariadb.Campaigns
	reviews   *mariadb.Reviews
	events    *mariadb.Events
	db        *gorm.DB
}

// newStores оборачивает соединение тремя хранилищами.
func newStores(db *gorm.DB) *testStores {
	return &testStores{
		campaigns: mariadb.NewCampaigns(db),
		reviews:   mariadb.NewReviews(db),
		events:    mariadb.NewEvents(db),
		db:        db,
	}
}

// newTestStore заводит отдельную базу MariaDB на тест (создаёт и убирает её
// testdb): тесты изолированы друг от друга.
func newTestStore(t *testing.T) *testStores {
	t.Helper()
	db, _ := testdb.New(t)
	return newStores(db)
}

// newEmptyStore — база без применённой схемы: нужна негативным проверкам.
func newEmptyStore(t *testing.T) *testStores {
	t.Helper()
	db, _ := testdb.NewEmpty(t)
	return newStores(db)
}

// openStores открывает соединение по готовому адресу: нужно проверкам «данные
// пережили перезапуск» — базу и схему готовит testdb, соединения открывает тест.
func openStores(t *testing.T, dsn string) *testStores {
	t.Helper()
	db, err := pool.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close(db) })
	return newStores(db)
}

func TestRecoverInterrupted(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

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

	n, err := mariadb.RecoverInterrupted(ctx, st.db)
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

	again, err := mariadb.RecoverInterrupted(ctx, st.db)
	if err != nil {
		t.Fatalf("recover again: %v", err)
	}
	if again != 0 {
		t.Errorf("second recover count = %d, want 0", again)
	}
}

// InnoDB проверяет внешние ключи всегда: деливерабл с несуществующей кампанией
// не вставится — как раньше с pragma foreign_keys=1 в SQLite.
func TestForeignKeysEnforced(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.db.WithContext(ctx).
		Exec(`INSERT INTO deliverables (id, campaign_id, topic, title, body, cta, review)
		 VALUES ('d-1','00000000-0000-0000-0000-00000000dead','t','a','b','c','{}')`).Error; err == nil {
		t.Fatal("ожидали ошибку FOREIGN KEY")
	}
}

// Прогресс пишется из параллельных горутин (по теме на горутину): InnoDB
// разводит конкурентные UPDATE сам, «database is locked» быть не должно.
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
