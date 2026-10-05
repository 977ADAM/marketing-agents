// Package testdb готовит изолированную базу MariaDB для тестов: заводит
// временную базу на каждый тест, применяет к ней миграции и убирает за собой.
//
// Это тестовая поддержка, а не часть приложения: прод-код её не импортирует
// (см. план раскладки §5 — тест-двойники живут вне пакетов-продюсеров).
//
// Сервер задаётся переменной TEST_DATABASE_URL (например
// mysql://root:secret@127.0.0.1:3306/ — база в адресе не нужна, её создаём сами).
// Если переменной нет, тесты пропускаются с понятным сообщением: `go test ./...`
// работает и без Docker, а `make test-backend` поднимает MariaDB и задаёт адрес.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/977ADAM/marketing-agents/internal/core/repository/mariadb/pool"
)

// EnvVar — переменная окружения с адресом сервера MariaDB для тестов.
const EnvVar = "TEST_DATABASE_URL"

// New заводит временную базу и применяет к ней миграции — это то, что делает
// сервис migrate в проде.
func New(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	db, dsn := NewEmpty(t)
	applyMigrations(t, db)
	return db, dsn
}

// NewDSN заводит временную базу со схемой и отдаёт только адрес: нужен тестам,
// которые сами открывают и закрывают соединения (проверки «данные пережили
// перезапуск»).
func NewDSN(t *testing.T) string {
	t.Helper()
	db, dsn := New(t)
	_ = pool.Close(db)
	return dsn
}

// NewEmpty заводит временную базу без схемы: нужна негативным проверкам
// (например «миграции не применены»).
func NewEmpty(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	base := serverURL(t)

	admin, err := pool.Open(context.Background(), base)
	if err != nil {
		t.Fatalf("подключение к MariaDB (%s): %v", pool.Target(base), err)
	}
	t.Cleanup(func() { _ = pool.Close(admin) })

	name := "ma_test_" + randomSuffix()
	// База из прошлого прогона с тем же именем (теоретически) не должна помешать.
	if err := admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error; err != nil {
		t.Fatalf("drop database %s: %v", name, err)
	}
	if err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error; err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	dsn := withDatabase(base, name)
	db, err := pool.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("подключение к %s: %v", pool.Target(dsn), err)
	}
	t.Cleanup(func() { _ = pool.Close(db) })
	return db, dsn
}

// serverURL читает адрес сервера из env и убирает из него имя базы: базы тесты
// создают сами.
func serverURL(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(EnvVar))
	if raw == "" {
		t.Skipf("%s не задан — тесты с MariaDB пропущены (make test-backend поднимает базу)", EnvVar)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", EnvVar, err)
	}
	u.Path = ""
	return u.String()
}

// withDatabase подставляет в адрес имя базы.
func withDatabase(base, name string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.Path = "/" + name
	return u.String()
}

// randomSuffix — случайный хвост имени базы: тесты не мешают друг другу.
func randomSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// applyMigrations применяет секции `-- migrate:up` из backend/migrations/*.sql по
// порядку и заводит таблицу учёта версий — ту же, что создаёт dbmate.
func applyMigrations(t *testing.T, db *gorm.DB) {
	t.Helper()
	versions := make([]string, 0, 4)
	for _, file := range migrationFiles(t) {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("чтение %s: %v", file, err)
		}
		up, ok := upSection(string(body))
		if !ok {
			t.Fatalf("%s: нет секции -- migrate:up", file)
		}
		for _, stmt := range splitStatements(up) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("применение %s: %v\n%s", file, err, stmt)
			}
		}
		versions = append(versions, versionOf(file))
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version varchar(128) PRIMARY KEY
	)`).Error; err != nil {
		t.Fatalf("таблица учёта миграций: %v", err)
	}
	for _, v := range versions {
		if err := db.Exec(`INSERT IGNORE INTO schema_migrations (version) VALUES (?)`, v).Error; err != nil {
			t.Fatalf("версия схемы %s: %v", v, err)
		}
	}
}

// migrationFiles находит backend/migrations/*.sql от корня модуля: тесты идут из
// каталога своего пакета, поэтому поднимаемся до go.mod.
func migrationFiles(t *testing.T) []string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("корень модуля: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("поиск миграций: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("миграции не найдены: ожидались backend/migrations/*.sql")
	}
	sort.Strings(files)
	return files
}

// moduleRoot ищет каталог с go.mod вверх от текущего каталога.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// upSection вырезает SQL между `-- migrate:up` и `-- migrate:down`.
func upSection(body string) (string, bool) {
	const upMarker, downMarker = "-- migrate:up", "-- migrate:down"
	i := strings.Index(body, upMarker)
	if i < 0 {
		return "", false
	}
	rest := body[i+len(upMarker):]
	if j := strings.Index(rest, downMarker); j >= 0 {
		rest = rest[:j]
	}
	return rest, true
}

// versionOf — версия миграции: ведущие цифры имени файла (0001_init.sql → 0001).
func versionOf(name string) string {
	base := filepath.Base(name)
	i := 0
	for i < len(base) && base[i] >= '0' && base[i] <= '9' {
		i++
	}
	return base[:i]
}

// splitStatements режет секцию миграции на отдельные операторы по ';': драйвер
// выполняет по одному оператору за раз (multiStatements у нас выключен).
// Миграции проекта — простые DDL/DML без ';' внутри строк, этого достаточно;
// если появятся процедуры или триггеры, резать придётся аккуратнее.
func splitStatements(script string) []string {
	parts := strings.Split(script, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
