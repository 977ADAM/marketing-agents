// Package mariadb — репозитории поверх MariaDB на GORM: модели таблиц, запросы и
// перевод строк в доменные типы.
//
// Соединение открывает internal/core/repository/mariadb/pool — здесь его только
// используют. Порты объявлены в доменных пакетах (campaign.Store, review.Store,
// trace.Store, trace.Sink), а имена файлов — по сущности, а не по слою.
//
// Модели (campaignRow, reviewRow, runEventRow) — внутренние: домен про GORM не
// знает, JSON-поля лежат текстом, а время — DATETIME(3) в UTC.
package mariadb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// DefaultClientID — клиент по умолчанию: кампании и проверки без явного client_id.
const DefaultClientID = "00000000-0000-0000-0000-000000000001"

// interruptedMsg — чем помечается прогон, переживший рестарт сервиса.
const interruptedMsg = "прервано рестартом сервиса"

// nowUTC — момент записи в UTC: колонки DATETIME(3) хранят время без таймзоны.
func nowUTC() time.Time { return time.Now().UTC() }

// RecoverInterrupted помечает осиротевшие после рестарта кампании и проверки
// (pending/running) как failed. Возвращает общее число восстановленных. Идемпотентен.
func RecoverInterrupted(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	for _, model := range []any{&campaignRow{}, &reviewRow{}} {
		res := db.WithContext(ctx).Model(model).
			Where("status IN ?", []string{"pending", "running"}).
			Updates(map[string]any{"status": "failed", "error": interruptedMsg, "updated_at": nowUTC()})
		if res.Error != nil {
			return 0, res.Error
		}
		total += res.RowsAffected
	}
	return total, nil
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
