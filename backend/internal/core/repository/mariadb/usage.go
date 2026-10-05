package mariadb

import (
	"context"
	llm "github.com/977ADAM/marketing-agents/internal/core/llm"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	"gorm.io/gorm"
	"time"
)

func AppendUsage(ctx context.Context, db *gorm.DB, table, kind, id string, e llm.UsageEntry, now time.Time) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := CheckFence(ctx, tx, table, id, now); err != nil {
			return err
		}
		owner, _ := run.Owner(ctx)
		return tx.Exec("INSERT INTO run_usage (id,run_kind,run_id,attempt,model,role,prompt_tokens,completion_tokens) VALUES (?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id", e.ID, kind, id, owner.Attempt, e.Model, e.Role, e.PromptTokens, e.CompletionTokens).Error
	})
}
func UsageEntries(ctx context.Context, db *gorm.DB, kind, id string) ([]llm.UsageEntry, error) {
	var entries []llm.UsageEntry
	err := db.WithContext(ctx).Table("run_usage").Where("run_kind=? AND run_id=?", kind, id).Order("created_at,id").Find(&entries).Error
	return entries, err
}
