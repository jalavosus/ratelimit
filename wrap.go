package ratelimit

import (
	"context"
	"time"
)

// Wrap wraps the passed function within another function, which no parameters
// and calls RateLimit using fn(). Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimit around every function call.
func Wrap(fn RateLimiterFn, callsPerInterval int64, interval time.Duration) WrappedFn {
	r := NewRateLimiter(callsPerInterval, interval)

	return func() error {
		return r.RateLimit(fn)
	}
}

// WrapContext wraps the passed function within another function, which takes a single context.Context parameter
// and calls RateLimitContext using fn() and that context. Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimitContext around every function call.
func WrapContext(fn RateLimiterFn, callsPerInterval int64, interval time.Duration) WrappedFnContext {
	r := NewRateLimiter(callsPerInterval, interval)

	return func(ctx context.Context) error {
		return r.RateLimitContext(ctx, fn)
	}
}
