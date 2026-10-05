package mariadb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type CheckpointRow struct {
	RunKind, RunID, Stage string
	Position              int
	InputHash, Payload    string
	UpdatedAt             time.Time
}

func (CheckpointRow) TableName() string { return "run_checkpoints" }
func LoadCheckpoint(ctx context.Context, db *gorm.DB, kind, id, stage string, pos int, out any) (bool, error) {
	var row CheckpointRow
	err := db.WithContext(ctx).Where("run_kind=? AND run_id=? AND stage=? AND position=?", kind, id, stage, pos).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(row.Payload), out); err != nil {
		return false, fmt.Errorf("checkpoint %s/%d: %w", stage, pos, err)
	}
	return true, nil
}
func SaveInitialCheckpoint(ctx context.Context, db *gorm.DB, kind, id string, input any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(&CheckpointRow{RunKind: kind, RunID: id, Stage: "input", Position: 0, InputHash: run.InputHash(input), Payload: string(data), UpdatedAt: time.Now().UTC()}).Error
}
func SaveCheckpoint(ctx context.Context, db *gorm.DB, table, kind, id, stage string, pos int, data any, now time.Time) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := CheckFence(ctx, tx, table, id, now); err != nil {
			return err
		}
		var input CheckpointRow
		err := tx.Where("run_kind=? AND run_id=? AND stage='input' AND position=0", kind, id).First(&input).Error
		if err != nil {
			return err
		}
		row := CheckpointRow{RunKind: kind, RunID: id, Stage: stage, Position: pos, InputHash: input.InputHash, Payload: string(encoded), UpdatedAt: now}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_kind"}, {Name: "run_id"}, {Name: "stage"}, {Name: "position"}}, DoUpdates: clause.AssignmentColumns([]string{"payload", "input_hash", "updated_at"})}).Create(&row).Error
	})
}
func HasInput(ctx context.Context, db *gorm.DB, kind, id string) (bool, error) {
	var count int64
	err := db.WithContext(ctx).Model(&CheckpointRow{}).Where("run_kind=? AND run_id=? AND stage='input' AND position=0", kind, id).Count(&count).Error
	return count > 0, err
}
func CheckpointRows(ctx context.Context, db *gorm.DB, kind, id, stage string) ([]CheckpointRow, error) {
	var rows []CheckpointRow
	err := db.WithContext(ctx).Where("run_kind=? AND run_id=? AND stage=?", kind, id, stage).Order("position").Find(&rows).Error
	return rows, err
}
func Requeue(ctx context.Context, db *gorm.DB, table, kind, id string, now time.Time) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct{ Status string }
		if err := tx.Table(table).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if row.Status != "failed" {
			return run.ErrInvalidState
		}
		found, err := HasInput(ctx, tx, kind, id)
		if err != nil {
			return err
		}
		if !found {
			return run.ErrResumeUnavailable
		}
		result := tx.Table(table).Where("id=? AND status='failed'", id).Updates(map[string]any{"status": "pending", "lease_owner": nil, "lease_until": nil, "error": nil, "progress": gorm.Expr("JSON_SET(COALESCE(progress, '{}'), '$.phase', 'pending')"), "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return run.ErrInvalidState
		}
		return nil
	})
}
