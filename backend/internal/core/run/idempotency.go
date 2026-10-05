package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrIdempotencyConflict = errors.New("idempotency key already used for different input")

type keyContext struct{}

func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, keyContext{}, key)
}
func IdempotencyKey(ctx context.Context) string {
	key, _ := ctx.Value(keyContext{}).(string)
	return key
}
func InputHash(input any) string {
	b, _ := json.Marshal(input)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
