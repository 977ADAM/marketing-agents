// Package pool — общее подключение к MariaDB на GORM: разбор адреса, DSN
// драйвера и пул соединений.
//
// Живёт в ядре (core) как общий ресурс: репозитории
// (features/*/repository/mariadb) получают отсюда готовый *gorm.DB и не знают, как
// он открыт — это единственное место, где приложение касается драйвера и
// настроек соединения. Ядро из-за этого зависит от драйвера MySQL и GORM: это
// осознанное исключение из правила «core без внешних зависимостей» (см. README).
package pool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	gosqlmysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DefaultAddr — адрес MariaDB, если в DATABASE_URL не указан хост.
const DefaultAddr = "127.0.0.1:3306"

// defaultConfig — настройки соединения, обязательные для приложения.
//
// parseTime + loc=UTC: DATETIME(3) читается и пишется как time.Time в UTC (GORM
// требует parseTime — иначе время приходит строками). time_zone='+00:00':
// CURRENT_TIMESTAMP(3) в схеме и время из Go означают одно и то же независимо от
// таймзоны сервера. timeTruncate=1ms: драйвер обрезает time.Time до точности
// колонки — иначе сравнение с индексированной DATETIME(3) теряет range-scan
// (см. README драйвера).
func defaultConfig() *gosqlmysql.Config {
	cfg := gosqlmysql.NewConfig()
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
	// разбирает итоговый DSN при открытии соединения.
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

// Open открывает соединение с MariaDB через GORM. Миграции не применяет: это
// отдельный шаг (сервис migrate в docker-compose или `make migrate`), а сервер до
// старта проверяет готовность схемы через schema.CheckSchema.
//
// Настройки: DSN из defaultConfig (parseTime, UTC, timeTruncate), пул на 8
// соединений с ограниченным временем жизни (сервер и посредники рвут
// простаивающие). GORM не логирует сам — структурированные логи ведёт приложение
// (slog), а ошибки возвращаются значениями. SkipDefaultTransaction: транзакции
// открываем только там, где нужна атомарность (результат кампании вместе со
// статьями). NowFunc — UTC, чтобы значения совпадали со схемой.
func Open(ctx context.Context, databaseURL string) (*gorm.DB, error) {
	dsn, err := DSN(databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                 logger.Discard,
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("mariadb: соединение с %s: %w", Target(databaseURL), err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("mariadb: соединение с %s: %w", Target(databaseURL), err)
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	sqlDB.SetConnMaxLifetime(3 * time.Minute)
	sqlDB.SetConnMaxIdleTime(time.Minute)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("mariadb: соединение с %s: %w", Target(databaseURL), err)
	}
	return db, nil
}

// Close закрывает пул соединений под GORM.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
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
func applyQuery(cfg *gosqlmysql.Config, q url.Values) {
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
