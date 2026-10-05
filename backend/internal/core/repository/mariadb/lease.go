package mariadb

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Fenced adds ownership conditions to a write; unowned legacy writes may only
// change unowned records. The table comes from repository code, never user input.
func Fenced(ctx context.Context, q *gorm.DB, now time.Time) *gorm.DB {
	if o, ok := run.Owner(ctx); ok {
		return q.Where("lease_owner = ? AND lease_attempt = ? AND lease_until > ?", o.Owner, o.Attempt, now)
	}
	return q.Where("lease_owner IS NULL")
}
func CheckFence(ctx context.Context, db *gorm.DB, table, id string, now time.Time) error {
	var row struct {
		LeaseOwner   *string
		LeaseAttempt int64
		LeaseUntil   *time.Time
	}
	if err := db.WithContext(ctx).Table(table).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&row).Error; err != nil {
		return err
	}
	o, owned := run.Owner(ctx)
	if !owned {
		if row.LeaseOwner != nil {
			return run.ErrLeaseLost
		}
		return nil
	}
	if row.LeaseOwner == nil || *row.LeaseOwner != o.Owner || row.LeaseAttempt != o.Attempt || row.LeaseUntil == nil || !row.LeaseUntil.After(now) {
		return run.ErrLeaseLost
	}
	return nil
}

func AcquireLease(ctx context.Context, db *gorm.DB, table, id, owner string, ttl time.Duration, now time.Time) (attempt int64, acquired bool, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(table).Where("id=? AND status IN ? AND (lease_owner IS NULL OR lease_until<=?)", id, []string{"pending", "running"}, now).Updates(map[string]any{"lease_owner": owner, "lease_attempt": gorm.Expr("lease_attempt+1"), "lease_until": now.Add(ttl), "status": "running", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		acquired = true
		return tx.Table(table).Select("lease_attempt").Where("id=? AND lease_owner=?", id, owner).Scan(&attempt).Error
	})
	return
}
func RenewLease(ctx context.Context, db *gorm.DB, table, id, owner string, attempt int64, ttl time.Duration, now time.Time) error {
	result := db.WithContext(ctx).Table(table).Where("id=? AND lease_owner=? AND lease_attempt=? AND lease_until>? AND status IN ?", id, owner, attempt, now, []string{"pending", "running"}).Update("lease_until", now.Add(ttl))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return run.ErrLeaseLost
	}
	return nil
}
func RecoverExpired(ctx context.Context, db *gorm.DB, table string, now time.Time, grace time.Duration) (int64, error) {
	result := db.WithContext(ctx).Table(table).Where("status IN ? AND ((lease_owner IS NOT NULL AND lease_until<=?) OR (lease_owner IS NULL AND created_at<=?))", []string{"pending", "running"}, now, now.Add(-grace)).Updates(map[string]any{"status": "failed", "error": "прервано рестартом сервиса", "progress": gorm.Expr("JSON_SET(COALESCE(progress, '{}'), '$.phase', 'failed')"), "updated_at": now})
	return result.RowsAffected, result.Error
}
