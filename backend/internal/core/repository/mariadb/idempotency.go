package mariadb

import (
	"context"
	"errors"
	"github.com/977ADAM/marketing-agents/internal/core/identity"
	"github.com/977ADAM/marketing-agents/internal/core/run"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type creationKey struct{ ClientID, RunKind, RequestKey, InputHash, RunID string }

func (creationKey) TableName() string { return "run_creation_keys" }
func LookupCreation(ctx context.Context, db *gorm.DB, client, kind, key, hash string) (string, error) {
	if client == "" {
		client = identity.DefaultClientID
	}
	var row creationKey
	err := db.WithContext(ctx).Where("client_id=? AND run_kind=? AND request_key=?", client, kind, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if row.InputHash != hash {
		return "", run.ErrIdempotencyConflict
	}
	return row.RunID, nil
}

// CreateOnce locks the unique ledger row and creates the run in the same transaction.
func CreateOnce(ctx context.Context, db *gorm.DB, client, kind, key, hash string, create func(*gorm.DB, string) error) (id string, created bool, err error) {
	if client == "" {
		client = identity.DefaultClientID
	}
	candidate := identity.NewUUID()
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO run_creation_keys (client_id,run_kind,request_key,input_hash,run_id) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE run_id=run_id", client, kind, key, hash, candidate).Error; err != nil {
			return err
		}
		var row creationKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("client_id=? AND run_kind=? AND request_key=?", client, kind, key).First(&row).Error; err != nil {
			return err
		}
		if row.InputHash != hash {
			return run.ErrIdempotencyConflict
		}
		id = row.RunID
		if id != candidate {
			return nil
		}
		if err := create(tx, id); err != nil {
			return err
		}
		created = true
		return nil
	})
	return
}
