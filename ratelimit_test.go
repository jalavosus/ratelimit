package ratelimit_test

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"

	"github.com/stoicturtle/ratelimit"
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
		// {
		// 	name:    "needs-limit-omfg",
		// 	resLen:  int(maxRequests * 30),
		// 	f:       limitHitterFn,
		// 	wantErr: false,
		// },
		// {
		// 	name:    "needs-limit-omfg-2",
		// 	resLen:  int(maxRequests * 60),
		// 	f:       limitHitterFn,
		// 	wantErr: false,
		// },
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

func TestRateLimiter_RateLimitContext(t *testing.T) {
	tests := []struct {
		name    string
		resLen  int
		f       func() float64
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "simple",
			resLen:  200,
			f:       simpleRateLimitFn,
			timeout: 30 * time.Second,
			wantErr: false,
		},
		{
			name:    "needs-limit",
			resLen:  int(maxRequests * 3),
			f:       limitHitterFn,
			timeout: 1500 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "exceeds-deadline",
			resLen:  int(maxRequests * 3),
			f:       limitHitterFn,
			timeout: 500 * time.Millisecond,
			wantErr: true,
		},
		// may-exceed-deadline test cases are allowed to exceed timeout without failing, since the rate limiter
		// can be variable in how fast it processes things.
		// That is to say, sometimes they pass without the timeout being exceeded, other times not.
		{
			name:    "may-exceed-deadline",
			resLen:  int(maxRequests * 2),
			f:       limitHitterFn,
			timeout: limitHitterSleepDuration,
			wantErr: true,
		},
		{
			name:    "may-exceed-deadline-2",
			resLen:  int(maxRequests * 2),
			f:       limitHitterFn,
			timeout: limitHitterSleepDuration + 1*time.Millisecond,
			wantErr: true,
		},
		{
			name:    "may-exceed-deadline-3",
			resLen:  int(maxRequests * 2),
			f:       limitHitterFn,
			timeout: limitHitterSleepDuration + 4*time.Millisecond,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resSlice = make([]float64, 0, tt.resLen)

			r := newRateLimiter()

			var (
				wg sync.WaitGroup
				ch = make(chan float64, tt.resLen)
			)

			f := func() error {
				defer wg.Done()
				ch <- tt.f()

				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			var g *errgroup.Group
			g, ctx = errgroup.WithContext(ctx)

			for i := 0; i < tt.resLen; i++ {
				wg.Add(1)
				g.Go(func() error {
					return r.RateLimitContext(ctx, f)
				})
			}

			go func() {
				defer close(ch)
				wg.Wait()
			}()

			doneCh := make(chan bool)

			go func(doneCh chan<- bool) {
				defer func() {
					doneCh <- true
				}()

				for res := range ch {
					resSlice = append(resSlice, res)
				}
			}(doneCh)

			waitGroupErr := g.Wait()

			if waitGroupErr != nil {
				if !tt.wantErr {
					t.Errorf("RateLimitContext() error = %v, wantErr %v", waitGroupErr, tt.wantErr)
					t.FailNow()
				} else {
					if errors.Is(waitGroupErr, context.DeadlineExceeded) {
						t.Log("context deadline exceeded, but it's okay")
					}

					return
				}
			}

			<-doneCh

			assert.Lenf(t, resSlice, tt.resLen, "resSlice has %d elements; wanted %d elements", len(resSlice), tt.resLen)
		})
	}
}

// func TestRateLimiter_SetInterval(t *testing.T) {
// 	type fields struct {
// 		callsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		bucketTimeout          time.Duration
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
// 					callsPerInterval: tt.fields.callsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					bucketTimeout:          tt.fields.bucketTimeout,
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
// 		callsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		bucketTimeout          time.Duration
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
// 					callsPerInterval: tt.fields.callsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					bucketTimeout:          tt.fields.bucketTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if got := r.SetCallsPerInterval(tt.args.rpi); !reflect.DeepEqual(got, tt.want) {
// 					t.Errorf("SetCallsPerInterval() = %v, want %v", got, tt.want)
// 				}
// 			},
// 		)
// 	}
// }

// func TestRateLimiter_SetSemaphoreTimeout(t *testing.T) {
// 	type fields struct {
// 		callsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		bucketTimeout          time.Duration
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
// 					callsPerInterval: tt.fields.callsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					bucketTimeout:          tt.fields.bucketTimeout,
// 					bucket:              tt.fields.bucket,
// 				}
// 				if got := r.SetBucketTimeout(tt.args.timeout); !reflect.DeepEqual(got, tt.want) {
// 					t.Errorf("SetBucketTimeout() = %v, want %v", got, tt.want)
// 				}
// 			},
// 		)
// 	}
// }

// func TestRateLimiter_Wrap(t *testing.T) {
// 	type fields struct {
// 		callsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		bucketTimeout          time.Duration
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
// 					callsPerInterval: tt.fields.callsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					bucketTimeout:          tt.fields.bucketTimeout,
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
// 		callsPerInterval int64
// 		interval            time.Duration
// 		mux                 *sync.RWMutex
// 		sem                 *semaphore.Weighted
// 		ticker              *time.Ticker
// 		bucketTimeout          time.Duration
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
// 					callsPerInterval: tt.fields.callsPerInterval,
// 					interval:            tt.fields.interval,
// 					mux:                 tt.fields.mux,
// 					sem:                 tt.fields.sem,
// 					ticker:              tt.fields.ticker,
// 					bucketTimeout:          tt.fields.bucketTimeout,
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
