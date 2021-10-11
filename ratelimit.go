package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// DefaultSemaphoreTimeout is the default amount of time that a call to RateLimiter.RateLimit will wait to acquire
// a semaphore before timing out and returning a context.DeadlineExceeded error.
//
// Note that this does *not* apply to RateLimiter.RateLimitContext when called with a programmer-created context.
const DefaultSemaphoreTimeout = time.Minute

type (
	// WrappedFn represents a function wrapped by a call to RateLimiter.RateLimit.
	WrappedFn func() error
	// WrappedFnContext represents a function wrapped by a call to RateLimiter.RateLimitContext.
	WrappedFnContext func(context.Context) error
)

// RateLimiter is a fancy little struct which can do everything you want it to and more
// in the realm of rate limiting.
type RateLimiter struct {
	requestsPerInterval int64
	interval            time.Duration
	mux                 *sync.RWMutex
	sem                 *semaphore.Weighted
	ticker              *time.Ticker
	semTimeout          time.Duration
	bucket              *bucket
}

// NewRateLimiter returns a RateLimiter configured with the number of allowed requests per interval, as well as the interval.
//
// Examples:
// - api A allows 250 requests per second. Call NewRateLimiter(250, time.Second).
// - api B allowed 50 requests per 5 minutes. Call NewRateLimiter(50, 5*time.Minute).
func NewRateLimiter(requestsPerInterval int64, interval time.Duration) *RateLimiter {
	r := &RateLimiter{
		requestsPerInterval: requestsPerInterval,
		interval:            interval,
		mux:                 new(sync.RWMutex),
		sem:                 semaphore.NewWeighted(requestsPerInterval),
		ticker:              time.NewTicker(interval),
		semTimeout:          DefaultSemaphoreTimeout,
		bucket:              newBucket(requestsPerInterval),
	}

	return r
}

// RateLimit calls RateLimitContext using a "default" context, which is a context.Context using the RateLimiter's current semaphore timeout
// as the time.Duration passed to context.WithTimeout (by default, this is set to DefaultSemaphoreTimeout).
//
// Param f: function with no parameters which returns an error. This is usually wrapped around the function call
// one is actually making.
//
// Return: error, if one occurs either from calling f() or from the context
// Otherwise, any error returned from calling f() is returned.
func (r *RateLimiter) RateLimit(f func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.semTimeout)
	defer cancel()

	return r.RateLimitContext(ctx, f)
}

// RateLimitContext calls the passed function f using the passed context.Context.
// If the passed context's deadline is exceeded while waiting to acquire a semaphore, a wrapped error is returned.
// Otherwise, the result of calling f() is returned.
func (r *RateLimiter) RateLimitContext(ctx context.Context, f func() error) error {
	var g *errgroup.Group

	g, ctx = errgroup.WithContext(ctx)

	gFunc := func() error {
		if semErr := r.sem.Acquire(ctx, 1); semErr != nil {
			if errors.Is(semErr, context.DeadlineExceeded) {
				return errors.Wrap(semErr, "context deadline exceeded waiting to acquire semaphore")
			}

			return errors.Wrap(semErr, "error trying to acquire semaphore")
		}

		defer r.sem.Release(1)

		return r.withBucket(f)
	}

	g.Go(gFunc)

	err := g.Wait()

	return err
}

// Wrap wraps the passed function within another function, which no parameters
// and calls RateLimit using f(). Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimit around every function call.
func (r *RateLimiter) Wrap(f func() error) WrappedFn {
	return func() error {
		return r.RateLimit(f)
	}
}

// WrapContext wraps the passed function within another function, which takes a single context.Context parameter
// and calls RateLimitContext using f() and that context. Helpful for building functions which are "preloaded" with
// a rate limiter, rather than calling RateLimitContext around every function call.
func (r *RateLimiter) WrapContext(f func() error) WrappedFnContext {
	return func(ctx context.Context) error {
		return r.RateLimitContext(ctx, f)
	}
}

// RequestsPerInterval returns the current number of requests allowed to be made during
// a RateLimiter's interval.
func (r RateLimiter) RequestsPerInterval() int64 {
	return r.withReadLock(func() interface{} {
		return r.requestsPerInterval
	}).(int64)
}

// SetRequestsPerInterval sets the number of requests allowed to be made during the configured interval.
// Note that this function will block the RateLimiter's other functions until it finishes.
func (r *RateLimiter) SetRequestsPerInterval(rpi int64) *RateLimiter {
	r.withWriteLock(func() {
		r.requestsPerInterval = rpi
		r.setSemaphore(rpi)
		r.bucket.setMaxRequests(rpi)
	})

	return r
}

// Interval returns the current interval for a RateLimiter.
func (r RateLimiter) Interval() time.Duration {
	return r.withReadLock(func() interface{} {
		return r.interval
	}).(time.Duration)
}

// SetInterval sets the RateLimiter's interval within which the RateLimiter's requestsPerInterval number of
// requests are allowed to be made.
// Note that this function will block the RateLimiter's other functions until it finishes.
func (r *RateLimiter) SetInterval(interval time.Duration) *RateLimiter {
	r.withWriteLock(func() {
		r.interval = interval
		r.setTicker(r.interval)
	})

	return r
}

// SemaphoreTimeout returns the current configured timeout duration used for non-contexed calls to RateLimit,
// after which deadline the context will return a context.DeadlineExceeded error.
//
// By default, this is set to DefaultSemaphoreTimeout (1 minute).
func (r RateLimiter) SemaphoreTimeout() time.Duration {
	return r.withReadLock(func() interface{} {
		return r.semTimeout
	}).(time.Duration)
}

// SetSemaphoreTimeout sets the timeout duration used for non-contexed calls to RateLimit,
// after which deadline the context will return a context.DeadlineExceeded error.
// Note that this function will block the RateLimiter's other functions until it finishes.
func (r *RateLimiter) SetSemaphoreTimeout(timeout time.Duration) *RateLimiter {
	r.withWriteLock(func() {
		r.semTimeout = timeout
	})

	return r
}

func (r *RateLimiter) bucketFiller() {
	go func(r *RateLimiter) {
		for range r.ticker.C {
			r.withWriteLock(func() {
				r.setSemaphore(r.requestsPerInterval)
				r.bucket.setResetStateCleared()
				r.bucket.emptyBucket()
			})
		}
	}(r)
}

func (r *RateLimiter) setSemaphore(rpi int64) {
	r.sem = semaphore.NewWeighted(rpi)
}

func (r *RateLimiter) setTicker(interval time.Duration) {
	r.ticker.Reset(interval)
}

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

func (r *RateLimiter) withBucket(f func() error) error {
	r.bucket.addToBucket()
	defer r.bucket.removeFromBucket()

	return f()
}
