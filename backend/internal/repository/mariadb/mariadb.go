// Package mariadb — репозитории поверх MariaDB: SQL по сущностям и перевод строк
// в доменные типы.
//
// Соединение открывает internal/repository/mariadb/pool — здесь его только
// используют. Порты объявлены в доменных пакетах (campaign.Store, review.Store,
// trace.Store, trace.Sink), а имена файлов — по сущности, а не по слою.
package mariadb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// DefaultClientID — клиент по умолчанию: кампании и проверки без явного client_id.
const DefaultClientID = "00000000-0000-0000-0000-000000000001"

// nowExpr — SQL-выражение «сейчас» в UTC с точностью колонок DATETIME(3).
// UTC_TIMESTAMP не зависит от таймзоны сессии, поэтому значение не поедет, даже
// если соединение почему-то окажется не в UTC.
const nowExpr = `UTC_TIMESTAMP(3)`

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
