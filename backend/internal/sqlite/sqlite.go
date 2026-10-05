// Package sqlite — адаптер хранения на SQLite (modernc.org/sqlite, чистый Go).
//
// Здесь только SQL и перевод строк в доменные типы: порты объявлены в доменных
// пакетах (campaign.Store, review.Store, trace.Store, trace.Sink), а имена файлов
// — по сущности, а не по слою.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const DefaultClientID = "00000000-0000-0000-0000-000000000001"

// nowExpr — SQL-выражение «сейчас» в формате, который драйвер разбирает в
// time.Time (колонки объявлены как DATETIME): 'YYYY-MM-DD HH:MM:SS.mmm', UTC.
const nowExpr = `strftime('%Y-%m-%d %H:%M:%f','now')`

// pragmas — настройки соединения: ждать снятия блокировки при конкурентной
// записи, WAL (читатели не блокируют писателя), внешние ключи, меньше fsync.
// _txlock=immediate — транзакция на запись берёт блокировку сразу, поэтому
// работает busy_timeout, а не падает с SQLITE_BUSY_SNAPSHOT.
const pragmas = "_txlock=immediate&_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

// DSN собирает URI подключения к файлу БД с нужными pragma.
func DSN(path string) string {
	if strings.HasPrefix(path, "file:") {
		return path // вызывающая сторона передала готовый URI
	}
	if path == ":memory:" {
		// WAL для in-memory БД бессмысленен (SQLite оставляет journal_mode=memory).
		return "file::memory:?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	return "file:" + path + "?" + pragmas
}

// New оборачивает уже открытое соединение (без миграций).

// OpenDB открывает (при необходимости создаёт каталог и файл) соединение с БД.
// Миграции не применяет: это отдельный шаг — сервис migrate в docker-compose или
// `make migrate` (см. internal/migrate), а сервер до старта проверяет готовность
// схемы через migrate.CheckReady.

// OpenDB открывает (при необходимости создаёт каталог и файл) соединение с БД.
// Миграции не применяет: это отдельный шаг — сервис migrate в docker-compose или
// `make migrate` (см. internal/migrate), а сервер до старта проверяет готовность
// схемы через migrate.CheckReady.
func OpenDB(ctx context.Context, path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: empty sqlite path")
	}
	if err := ensureDir(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	return db, nil
}

// Close закрывает соединение с БД.

// ensureDir создаёт каталог под файл БД, если его нет.

// ensureDir создаёт каталог под файл БД, если его нет.
func ensureDir(path string) error {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: create dir %s: %w", dir, err)
	}
	return nil
}

// newUUID генерирует UUID v4.

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

// Create вставляет кампанию в статусе pending и возвращает её id.

// RecoverInterrupted// (pending/running) как failed. Возвращает общее число восстановленных. Идемпотентен.
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

// ListRecent возвращает до limit последних кампаний, новые сверху.
// rowid — тайбрейкер для записей с одинаковым created_at (точность — миллисекунды).
