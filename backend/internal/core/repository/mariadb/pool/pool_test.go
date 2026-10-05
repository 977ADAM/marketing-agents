package pool_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/977ADAM/marketing-agents/internal/core/repository/mariadb/pool"
	"github.com/go-sql-driver/mysql"
)

// DSN: обязательные настройки соединения на месте, чужая схема отвергается.
func TestDSN(t *testing.T) {
	dsn, err := pool.DSN("mysql://marketing:s%40cret@db:3307/marketing")
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
	dsn, err = pool.DSN("mysql://user:pass@db/marketing?timeout=9s")
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
}

// Target показывает адрес для логов: пароль туда попадать не должен.
func TestTargetHidesPassword(t *testing.T) {
	got := pool.Target("mysql://marketing:supersecret@db:3306/marketing")
	if strings.Contains(got, "supersecret") {
		t.Errorf("Target = %q: пароль утёк", got)
	}
	if !strings.Contains(got, "db:3306/marketing") {
		t.Errorf("Target = %q, want адрес с хостом и базой", got)
	}
}

// Open отвергает неподходящий адрес до похода в сеть: ошибка про схему, а не
// таймаут подключения.
func TestOpenRejectsBadURL(t *testing.T) {
	for _, raw := range []string{"", "sqlite:data/marketing.db", "/var/lib/marketing/marketing.db"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := pool.Open(context.Background(), raw); err == nil {
				t.Errorf("Open(%q): ожидали ошибку про адрес", raw)
			}
		})
	}
}
