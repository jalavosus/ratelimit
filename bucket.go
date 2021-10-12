package ratelimit

import (
	"context"

	"github.com/pkg/errors"
)

type bucket struct {
	bucketSize     int64
	bucketAddCh    chan int64
	bucketRemoveCh chan int64
	bucketResetCh  chan bool
}

func newBucket(bucketSize int64) *bucket {
	b := &bucket{
		bucketSize:     bucketSize,
		bucketAddCh:    make(chan int64),
		bucketRemoveCh: make(chan int64, 1),
		bucketResetCh:  make(chan bool, 1),
	}

	b.startBucket()

	return b
}

func (b *bucket) addToBucket(ctx context.Context) error {
	for {
		select {
		case <-b.bucketAddCh:
			return nil
		case <-ctx.Done():
			ctxErr := ctx.Err()
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return errors.Wrap(ctxErr, "context deadline exceeded waiting for open bucket space")
			}

			return ctxErr
		}
	}
}

func (b *bucket) removeFromBucket() {
	go func(b *bucket) { b.bucketRemoveCh <- 1 }(b)
}

func (b *bucket) emptyBucket() {
	b.bucketResetCh <- true
}

func (b *bucket) startBucket() {
	go func(b *bucket) {
		var (
			available = b.bucketSize
		)

		for {
			select {
			case <-b.bucketRemoveCh:
				available++
			case <-b.bucketResetCh:
				available = b.bucketSize
			default:
				if available == 0 {
					continue
				}

				available--
				b.bucketAddCh <- 1
			}
		}
	}(b)
}
