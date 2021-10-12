package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// TODO: does it make sense to call f() within a goroutine/errgroup if it's likely already being called from within (or as) a goroutine?
// TODO: potentially implement a separate set of functions which don't fire off new goroutines to accompany RateLimit and RateLimitContext
// TODO: implement public, non-struct Wrap() and WrapContext() functions, with the understanding that they won't be able to modify the underlying RateLimiter.

const (
	rateLimitCtxKey string = "edforceone"
	rateLimitCtxVal string = "rateLimit42"
)

// DefaultBucketTimeout is the default amount of time that a call to RateLimiter.RateLimit will wait to acquire
// some bucket space before timing out and returning a context.DeadlineExceeded error.
//
// Note that this does *not* apply to RateLimiter.RateLimitContext when called with a programmer-created context.
const DefaultBucketTimeout = time.Minute

type (
	// WrappedFn represents a function wrapped by a call to RateLimiter.RateLimit.
	WrappedFn func() error
	// WrappedFnContext represents a function wrapped by a call to RateLimiter.RateLimitContext.
	WrappedFnContext func(context.Context) error
	// RateLimiterFn is a function which takes no parameters, and returns an error.
	// It represents the function type passed to RateLimiter.RateLimit and RateLimiter.RateLimitContext.
	RateLimiterFn func() error
)

// Wrap wraps the passed function within another function, which no parameters
// and calls RateLimit using f(). Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimit around every function call.
func Wrap(f func() error, callsPerInterval int64, interval time.Duration) WrappedFn {
	r := NewRateLimiter(callsPerInterval, interval)

	return func() error {
		return r.RateLimit(f)
	}
}

// WrapContext wraps the passed function within another function, which takes a single context.Context parameter
// and calls RateLimitContext using f() and that context. Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimitContext around every function call.
func WrapContext(f func() error, callsPerInterval int64, interval time.Duration) WrappedFnContext {
	r := NewRateLimiter(callsPerInterval, interval)

	return func(ctx context.Context) error {
		return r.RateLimitContext(ctx, f)
	}
}

// RateLimiter is a fancy little struct which can do everything you want it to and more
// in the realm of rate limiting.
type RateLimiter struct {
	callsPerInterval int64
	interval         time.Duration
	mux              *sync.RWMutex
	ticker           *time.Ticker
	bucketTimeout    time.Duration
	bucket           *bucket
}

// NewRateLimiter returns a RateLimiter configured with the number of allowed function calls per interval, as well as the interval.
//
// Examples:
// - api A allows 250 calls per second. Call NewRateLimiter(250, time.Second).
// - api B allowed 50 calls per 5 minutes. Call NewRateLimiter(50, 5*time.Minute).
func NewRateLimiter(callsPerInterval int64, interval time.Duration) *RateLimiter {
	r := &RateLimiter{
		callsPerInterval: callsPerInterval,
		interval:         interval,
		mux:              new(sync.RWMutex),
		ticker:           time.NewTicker(interval),
		bucketTimeout:    DefaultBucketTimeout,
		bucket:           newBucket(callsPerInterval),
	}

	return r
}

// RateLimit calls RateLimitContext using a "default" context, which is a context.Context using the RateLimiter's current bucket timeout
// as the time.Duration passed to context.WithTimeout (by default, this is set to DefaultBucketTimeout).
//
// Param fn: function with no parameters which returns an error (see RateLimiterFn). This is usually wrapped around the function call
// one is actually making.
//
// Return: error, if one occurs either from calling fn() or from the context.
// Otherwise, any error returned from calling fn() is returned.
func (r *RateLimiter) RateLimit(fn RateLimiterFn) error {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	ctx = context.WithValue(context.Background(), rateLimitCtxKey, rateLimitCtxVal)

	ctx, cancel = context.WithTimeout(context.Background(), r.bucketTimeout)
	defer cancel()

	return r.RateLimitContext(ctx, fn)
}

// RateLimitContext calls the passed function fn using the passed context.Context.
// If the passed context's deadline is exceeded while waiting to acquire bucket space, a wrapped error is returned.
// Otherwise, the result of calling fn() is returned.
func (r *RateLimiter) RateLimitContext(ctx context.Context, fn RateLimiterFn) error {
	var g *errgroup.Group

	g, ctx = errgroup.WithContext(ctx)

	gFunc := func() error {
		return r.withBucket(ctx, fn)
	}

	g.Go(gFunc)

	err := g.Wait()

	return err
}

// Wrap wraps the passed function within another function, which no parameters
// and calls RateLimit using fn(). Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimit around every function call.
func (r *RateLimiter) Wrap(fn RateLimiterFn) WrappedFn {
	return func() error {
		return r.RateLimit(fn)
	}
}

// WrapContext wraps the passed function within another function, which takes a single context.Context parameter
// and calls RateLimitContext using fn() and that context. Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimitContext around every function call.
func (r *RateLimiter) WrapContext(fn RateLimiterFn) WrappedFnContext {
	return func(ctx context.Context) error {
		return r.RateLimitContext(ctx, fn)
	}
}

// CallsPerInterval returns the current number of calls allowed to be made during
// a RateLimiter's interval.
func (r RateLimiter) CallsPerInterval() int64 {
	return r.withReadLock(func() interface{} {
		return r.callsPerInterval
	}).(int64)
}

// SetCallsPerInterval sets the number of calls allowed to be made during the configured interval.
// Note that this function will block the RateLimiter's other functions until it finishes.
func (r *RateLimiter) SetCallsPerInterval(n int64) *RateLimiter {
	r.withWriteLock(func() {
		r.callsPerInterval = n
		r.bucket.setBucketSize(n)
	})

	return r
}

// Interval returns the current interval for a RateLimiter.
func (r RateLimiter) Interval() time.Duration {
	return r.withReadLock(func() interface{} {
		return r.interval
	}).(time.Duration)
}

// SetInterval sets the RateLimiter's interval within which the RateLimiter's callsPerInterval number of
// calls are allowed to be made.
// Note that this function will block the RateLimiter's other functions until it finishes.
func (r *RateLimiter) SetInterval(interval time.Duration) *RateLimiter {
	r.withWriteLock(func() {
		r.interval = interval
		r.setTicker(r.interval)
	})

	return r
}

// BucketTimeout returns the current configured timeout duration used for non-contexed calls to RateLimit,
// after which deadline the context will return a context.DeadlineExceeded error.
//
// By default, this is set to DefaultBucketTimeout (1 minute).
func (r RateLimiter) BucketTimeout() time.Duration {
	return r.withReadLock(func() interface{} {
		return r.bucketTimeout
	}).(time.Duration)
}

// SetBucketTimeout sets the timeout duration used for non-contexed calls to RateLimit,
// after which deadline the context will return a context.DeadlineExceeded error.
// Note that this function will block the RateLimiter's other functions until it finishes.
func (r *RateLimiter) SetBucketTimeout(timeout time.Duration) *RateLimiter {
	r.withWriteLock(func() {
		r.bucketTimeout = timeout
	})

	return r
}

func (r *RateLimiter) bucketFiller() {
	go func(r *RateLimiter) {
		for range r.ticker.C {
			r.bucket.emptyBucket()
		}
	}(r)
}

func (r *RateLimiter) setTicker(interval time.Duration) {
	r.ticker.Reset(interval)
}

// I'm truly amazed that this works.
func (r *RateLimiter) withBucket(ctx context.Context, fn RateLimiterFn) error {
	// not quite sure what to do with this yet
	// if r.checkRateLimiterCtx(ctx) {
	// }

	// if this returns an error, it's likely a wrapped context.DeadlineExceeded error.
	if err := r.bucket.addToBucket(ctx); err != nil {
		return err
	}

	defer r.bucket.removeFromBucket()

	return fn()
}

// Below are functions which wrap other functions, similar to python's `with` scope/context management system.

func (r RateLimiter) withReadLock(f func() interface{}) interface{} {
	r.mux.RLock()
	defer r.mux.RUnlock()

	return f()
}

func (r *RateLimiter) withWriteLock(f func()) {
	r.mux.Lock()
	defer r.mux.Unlock()

	f()
}

func (r RateLimiter) checkRateLimiterCtx(ctx context.Context) bool {
	// check if context was created by r.RateLimit instead of
	// a context which was passed from elsewhere
	if ctxVal := ctx.Value(rateLimitCtxKey); ctxVal != nil {
		val, ok := ctxVal.(string)
		return ok && val == rateLimitCtxVal
	}

	return false
}
