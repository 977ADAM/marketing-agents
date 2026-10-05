package mariadb_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/mariadb"
	"github.com/977ADAM/marketing-agents/internal/run"
	"github.com/977ADAM/marketing-agents/internal/testdb"
	"github.com/go-sql-driver/mysql"
)

// testStores — три хранилища поверх одной БД: адаптер разделён по сущностям,
// поэтому и в тестах у каждой свой вход.
type testStores struct {
	campaigns *mariadb.Campaigns
	reviews   *mariadb.Reviews
	events    *mariadb.Events
	db        *sql.DB
}

// newStores оборачивает соединение тремя хранилищами.
func newStores(db *sql.DB) *testStores {
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
	db, err := mariadb.OpenDB(context.Background(), dsn)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
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
	if _, err := st.db.ExecContext(ctx,
		`INSERT INTO deliverables (id, campaign_id, topic, title, body, cta, review)
		 VALUES ('d-1','00000000-0000-0000-0000-00000000dead','t','a','b','c','{}')`); err == nil {
		t.Fatal("ожидали ошибку FOREIGN KEY")
	}
}

// DSN: обязательные настройки соединения на месте, чужая схема отвергается.
func TestDSN(t *testing.T) {
	dsn, err := mariadb.DSN("mysql://marketing:s%40cret@db:3307/marketing")
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	// Разбираем итоговый DSN драйвером: так проверяются значения полей, а не
	// строки в адресе (драйвер, например, не пишет loc=UTC — это его дефолт).
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%q): %v", dsn, err)
	}
	if cfg.User != "marketing" || cfg.Passwd != "s@cret" {
		t.Errorf("креды = %q/%q, want marketing/s@cret", cfg.User, cfg.Passwd)
	}
	if cfg.Addr != "db:3307" || cfg.DBName != "marketing" {
		t.Errorf("адрес = %q/%q, want db:3307/marketing", cfg.Addr, cfg.DBName)
	}
	if !cfg.ParseTime {
		t.Error("parseTime выключен: DATETIME(3) не прочитается в time.Time")
	}
	if cfg.Loc != time.UTC {
		t.Errorf("Loc = %v, want UTC", cfg.Loc)
	}
	if got := cfg.Params["time_zone"]; got != "'+00:00'" {
		t.Errorf("time_zone = %q, want сессию в UTC", got)
	}
	for _, want := range []string{"collation=utf8mb4_unicode_ci", "timeTruncate=1ms"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN = %q, нет %q", dsn, want)
		}
	}

	// Порт по умолчанию и параметры драйвера из адреса.
	dsn, err = mariadb.DSN("mysql://user:pass@db/marketing?timeout=9s")
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	cfg, err = mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%q): %v", dsn, err)
	}
	if cfg.Addr != "db:3306" {
		t.Errorf("Addr = %q, want порт 3306 по умолчанию", cfg.Addr)
	}
	if cfg.Timeout != 9*time.Second {
		t.Errorf("Timeout = %v, параметр из адреса потерялся", cfg.Timeout)
	}

	if _, err := mariadb.DSN("sqlite:data/marketing.db"); err == nil {
		t.Error("sqlite-адрес должен отвергаться с понятной ошибкой")
	}
	if _, err := mariadb.DSN(""); err == nil {
		t.Error("пустой адрес должен отвергаться")
	}
}

// Target показывает адрес для логов: пароль туда попадать не должен.
func TestTargetHidesPassword(t *testing.T) {
	got := mariadb.Target("mysql://marketing:supersecret@db:3306/marketing")
	if strings.Contains(got, "supersecret") {
		t.Errorf("Target = %q: пароль утёк", got)
	}
	if !strings.Contains(got, "db:3306/marketing") {
		t.Errorf("Target = %q, want адрес с хостом и базой", got)
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
