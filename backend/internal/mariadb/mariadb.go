// Package mariadb — адаптер хранения на MariaDB (драйвер go-sql-driver/mysql,
// чистый Go, CGO не нужен).
//
// Здесь только SQL и перевод строк в доменные типы: порты объявлены в доменных
// пакетах (campaign.Store, review.Store, trace.Store, trace.Sink), а имена файлов
// — по сущности, а не по слою.
package mariadb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// DefaultClientID — клиент по умолчанию: кампании и проверки без явного client_id.
const DefaultClientID = "00000000-0000-0000-0000-000000000001"

// nowExpr — SQL-выражение «сейчас» в UTC с точностью колонок DATETIME(3).
// UTC_TIMESTAMP не зависит от таймзоны сессии, поэтому значение не поедет, даже
// если соединение почему-то окажется не в UTC.
const nowExpr = `UTC_TIMESTAMP(3)`

// DefaultAddr — адрес MariaDB, если в DATABASE_URL не указан хост.
const DefaultAddr = "127.0.0.1:3306"

// defaultConfig — настройки соединения, обязательные для приложения.
//
// parseTime + loc=UTC: DATETIME(3) читается и пишется как time.Time в UTC.
// time_zone='+00:00': CURRENT_TIMESTAMP(3) в схеме и время из Go означают одно и
// то же независимо от таймзоны сервера. timeTruncate=1ms: драйвер обрезает
// time.Time до точности колонки — иначе сравнение с индексированной DATETIME(3)
// теряет range-scan (см. README драйвера).
func defaultConfig() *mysql.Config {
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = DefaultAddr
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Collation = "utf8mb4_unicode_ci"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	// Параметры соединения. time_zone — сессия в UTC. timeTruncate — у драйвера
	// это неэкспортируемое поле, поэтому задаём его строкой DSN: драйвер заново
	// разбирает итоговый DSN при открытии соединения. Без него time.Time уходит в
	// DATETIME(3) с наносекундами, MariaDB округляет значение, и сравнение с
	// индексированной колонкой теряет range-scan.
	cfg.Params = map[string]string{
		"time_zone":    "'+00:00'",
		"timeTruncate": "1ms",
	}
	return cfg
}

// DSN переводит DATABASE_URL вида mysql://user:pass@host:port/dbname в DSN
// драйвера go-sql-driver/mysql, добавляя обязательные настройки соединения.
// Параметры запроса понимаются как настройки драйвера, а неизвестные —
// как системные переменные соединения (как в самом драйвере).
func DSN(databaseURL string) (string, error) {
	u, err := parseURL(databaseURL)
	if err != nil {
		return "", err
	}
	cfg := defaultConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	if u.Host != "" {
		cfg.Addr = addrWithPort(u.Host)
	}
	cfg.DBName = strings.TrimPrefix(u.Path, "/")
	applyQuery(cfg, u.Query())
	return cfg.FormatDSN(), nil
}

// Target возвращает адрес БД без пароля — для логов и сообщений об ошибках.
func Target(databaseURL string) string {
	u, err := parseURL(databaseURL)
	if err != nil {
		return "mysql://?"
	}
	host := u.Host
	if host == "" {
		host = DefaultAddr
	}
	user := u.User.Username()
	if user == "" {
		user = "?"
	}
	return fmt.Sprintf("mysql://%s@%s%s", user, host, u.Path)
}

// parseURL разбирает DATABASE_URL и требует схему mysql:// (MariaDB использует
// её же: так один и тот же адрес понимают и приложение, и dbmate).
func parseURL(databaseURL string) (*url.URL, error) {
	raw := strings.TrimSpace(databaseURL)
	if raw == "" {
		return nil, errors.New("mariadb: пустой DATABASE_URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("mariadb: разбор DATABASE_URL: %w", err)
	}
	switch u.Scheme {
	case "mysql", "mariadb":
	default:
		return nil, fmt.Errorf(
			"mariadb: DATABASE_URL должен начинаться с mysql:// (MariaDB использует ту же схему), получено %q",
			redactURL(raw))
	}
	return u, nil
}

// addrWithPort дополняет хост портом по умолчанию, если порт не задан.
func addrWithPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	// IPv6-литерал без порта: [::1] → ::1.
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	return net.JoinHostPort(host, "3306")
}

// applyQuery переносит параметры URL: известные настройки драйвера — в поля
// Config, остальные — в системные переменные соединения.
func applyQuery(cfg *mysql.Config, q url.Values) {
	for k, vs := range q {
		if len(vs) == 0 {
			continue
		}
		v := vs[0]
		switch k {
		case "parseTime":
			setBool(v, &cfg.ParseTime)
		case "interpolateParams":
			setBool(v, &cfg.InterpolateParams)
		case "tls":
			cfg.TLSConfig = v
		case "collation":
			cfg.Collation = v
		case "charset":
			cfg.Params["charset"] = v
		case "timeout":
			setDur(v, &cfg.Timeout)
		case "readTimeout":
			setDur(v, &cfg.ReadTimeout)
		case "writeTimeout":
			setDur(v, &cfg.WriteTimeout)
		case "timeTruncate":
			cfg.Params["timeTruncate"] = v
		default:
			cfg.Params[k] = v
		}
	}
}

func setBool(v string, dst *bool) {
	if b, err := strconv.ParseBool(v); err == nil {
		*dst = b
	}
}

func setDur(v string, dst *time.Duration) {
	if d, err := time.ParseDuration(v); err == nil {
		*dst = d
	}
}

// OpenDB открывает соединение с MariaDB по DATABASE_URL. Миграции не применяет:
// это отдельный шаг (сервис migrate в docker-compose или `make migrate`), а
// сервер до старта проверяет готовность схемы через CheckSchema.
func OpenDB(ctx context.Context, databaseURL string) (*sql.DB, error) {
	dsn, err := DSN(databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mariadb: открытие соединения: %w", err)
	}
	// Пул: соединения не живут вечно (сервер и посредники рвут простаивающие), но
	// с запасом под параллельную запись прогресса по темам.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(3 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mariadb: соединение с %s: %w", Target(databaseURL), err)
	}
	return db, nil
}

// RecoverInterrupted помечает осиротевшие после рестарта кампании и проверки
// (pending/running) как failed. Возвращает общее число восстановленных. Идемпотентен.
func RecoverInterrupted(ctx context.Context, db *sql.DB) (int64, error) {
	tag, err := db.ExecContext(ctx,
		`UPDATE campaigns SET status='failed', error='прервано рестартом сервиса', updated_at=`+nowExpr+`
		 WHERE status IN ('pending','running')`)
	if err != nil {
		return 0, err
	}
	n, err := tag.RowsAffected()
	if err != nil {
		return 0, err
	}
	tag, err = db.ExecContext(ctx,
		`UPDATE reviews SET status='failed', error='прервано рестартом сервиса', updated_at=`+nowExpr+`
		 WHERE status IN ('pending','running')`)
	if err != nil {
		return 0, err
	}
	m, err := tag.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n + m, nil
}

// newUUID генерирует UUID v4.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// redactURL прячет пароль в адресе БД: сообщения об ошибках попадают в логи.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "mysql://?"
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}
