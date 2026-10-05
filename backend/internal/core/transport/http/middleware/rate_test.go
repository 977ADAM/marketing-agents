package middleware

import "testing"

func TestZeroRateIsDisabled(t *testing.T) {
	r := NewRateLimiter(0)
	for i := 0; i < 100; i++ {
		if !r.Allow() {
			t.Fatal("disabled limiter blocked request")
		}
	}
}
func TestPositiveRateLimits(t *testing.T) {
	r := NewRateLimiter(1)
	if !r.Allow() || r.Allow() {
		t.Fatal("positive limiter ignored")
	}
}
