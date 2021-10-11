package ratelimit_test

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"

	"github.com/jalavosus/ratelimit"
)

const (
	maxRequests              int64 = 300
	requestInterval                = time.Second
	limitHitterSleepDuration       = 650 * time.Millisecond
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func newRateLimiter() *ratelimit.RateLimiter {
	return ratelimit.NewRateLimiter(maxRequests, requestInterval)
}

func simpleRateLimitFn() float64 {
	return rand.Float64()
}

func limitHitterFn() float64 {
	ch := make(chan bool, 1)

	// use a goroutine + channel to prevent weird cross-process fuckery
	go func(ch chan<- bool) {
		time.Sleep(limitHitterSleepDuration)
		ch <- true
	}(ch)

	<-ch

	return rand.Float64()
}

func TestRateLimiter_RateLimit(t *testing.T) {
	tests := []struct {
		name    string
		resLen  int
		f       func() float64
		wantErr bool
	}{
		{
			name:    "simple",
			resLen:  200,
			f:       simpleRateLimitFn,
			wantErr: false,
		},
		{
			name:    "needs-limit",
			resLen:  int(maxRequests * 3),
			f:       limitHitterFn,
			wantErr: false,
		},
		{
			name:    "needs-limit-omfg",
			resLen:  int(maxRequests * 30),
			f:       limitHitterFn,
			wantErr: false,
		},
		{
			name:    "needs-limit-omfg-2",
			resLen:  int(maxRequests * 60),
			f:       limitHitterFn,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resSlice = make([]float64, 0, tt.resLen)

			r := newRateLimiter()

			ch := make(chan float64, tt.resLen)

			var wg sync.WaitGroup

			f := func() error {
				defer wg.Done()
				ch <- tt.f()

				return nil
			}

			var g, _ = errgroup.WithContext(context.Background())

			for i := 0; i < tt.resLen; i++ {
				wg.Add(1)

				g.Go(func() error {
					return r.RateLimit(f)
				})
			}

			go func() {
				defer close(ch)
				wg.Wait()
			}()

			for res := range ch {
				resSlice = append(resSlice, res)
			}

			if err := g.Wait(); (err != nil) != tt.wantErr {
				t.Errorf("RateLimit() error = %v, wantErr %v", err, tt.wantErr)
				t.FailNow()
			}

			assert.Len(t, resSlice, tt.resLen)
		})
	}
}

// func TestRateLimiter_RateLimitContext(t *testing.T) {
// 	type fields struct {
// 		requestsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		semTimeout          time.Duration
// 		bucket              *bucket
// 	}
// 	type args struct {
// 		ctx context.Context
// 		f   func() error
// 	}
// 	tests := []struct {
// 		name    string
// 		fields  fields
// 		args    args
// 		wantErr bool
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(
// 			tt.name, func(t *testing.T) {
// 				r := &ratelimit.RateLimiter{
// 					requestsPerInterval: tt.fields.requestsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					semTimeout:          tt.fields.semTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if err := r.RateLimitContext(tt.args.ctx, tt.args.f); (err != nil) != tt.wantErr {
// 					t.Errorf("RateLimitContext() error = %v, wantErr %v", err, tt.wantErr)
// 				}
// 			},
// 		)
// 	}
// }

// func TestRateLimiter_SetInterval(t *testing.T) {
// 	type fields struct {
// 		requestsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		semTimeout          time.Duration
// 		bucket              *bucket
// 	}
// 	type args struct {
// 		interval time.Duration
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		args   args
// 		want   *RateLimiter
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(
// 			tt.name, func(t *testing.T) {
// 				r := &ratelimit.RateLimiter{
// 					requestsPerInterval: tt.fields.requestsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					semTimeout:          tt.fields.semTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if got := r.SetInterval(tt.args.interval); !reflect.DeepEqual(got, tt.want) {
// 					t.Errorf("SetInterval() = %v, want %v", got, tt.want)
// 				}
// 			},
// 		)
// 	}
// }

// func TestRateLimiter_SetRequestsPerInterval(t *testing.T) {
// 	type fields struct {
// 		requestsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		semTimeout          time.Duration
// 		bucket              *bucket
// 	}
// 	type args struct {
// 		rpi int64
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		args   args
// 		want   *RateLimiter
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(
// 			tt.name, func(t *testing.T) {
// 				r := &ratelimit.RateLimiter{
// 					requestsPerInterval: tt.fields.requestsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					semTimeout:          tt.fields.semTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if got := r.SetRequestsPerInterval(tt.args.rpi); !reflect.DeepEqual(got, tt.want) {
// 					t.Errorf("SetRequestsPerInterval() = %v, want %v", got, tt.want)
// 				}
// 			},
// 		)
// 	}
// }

// func TestRateLimiter_SetSemaphoreTimeout(t *testing.T) {
// 	type fields struct {
// 		requestsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		semTimeout          time.Duration
// 		bucket              *bucket
// 	}
// 	type args struct {
// 		timeout time.Duration
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		args   args
// 		want   *RateLimiter
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(
// 			tt.name, func(t *testing.T) {
// 				r := &ratelimit.RateLimiter{
// 					requestsPerInterval: tt.fields.requestsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					semTimeout:          tt.fields.semTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if got := r.SetSemaphoreTimeout(tt.args.timeout); !reflect.DeepEqual(got, tt.want) {
// 					t.Errorf("SetSemaphoreTimeout() = %v, want %v", got, tt.want)
// 				}
// 			},
// 		)
// 	}
// }

// func TestRateLimiter_Wrap(t *testing.T) {
// 	type fields struct {
// 		requestsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		semTimeout          time.Duration
// 		bucket              *bucket
// 	}
// 	type args struct {
// 		f func() error
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		args   args
// 		want   ratelimit.WrappedFn
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(
// 			tt.name, func(t *testing.T) {
// 				r := &ratelimit.RateLimiter{
// 					requestsPerInterval: tt.fields.requestsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					semTimeout:          tt.fields.semTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if got := r.Wrap(tt.args.f); !reflect.DeepEqual(got, tt.want) {
// 					t.Errorf("Wrap() = %v, want %v", got, tt.want)
// 				}
// 			},
// 		)
// 	}
// }

// func TestRateLimiter_WrapContext(t *testing.T) {
// 	type fields struct {
// 		requestsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		semTimeout          time.Duration
// 		bucket              *bucket
// 	}
// 	type args struct {
// 		f func() error
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		args   args
// 		want   ratelimit.WrappedFnContext
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(
// 			tt.name, func(t *testing.T) {
// 				r := &ratelimit.RateLimiter{
// 					requestsPerInterval: tt.fields.requestsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					semTimeout:          tt.fields.semTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if got := r.WrapContext(tt.args.f); !reflect.DeepEqual(got, tt.want) {
// 					t.Errorf("WrapContext() = %v, want %v", got, tt.want)
// 				}
// 			},
// 		)
// 	}
// }
//
