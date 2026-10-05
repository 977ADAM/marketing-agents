package middleware

import "golang.org/x/time/rate"

type RateLimiter struct{ limiter *rate.Limiter }

func NewRateLimiter(perMinute int) *RateLimiter {
	return &RateLimiter{rate.NewLimiter(rate.Limit(float64(perMinute)/60), perMinute)}
}
func (r *RateLimiter) Allow() bool { return r.limiter.Allow() }
