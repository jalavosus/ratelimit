package bucket

import (
	"context"

	"github.com/pkg/errors"
)

// Bucket is magic. Do not question the magic.
type Bucket struct {
	size      int64
	acquireCh chan int64
	releaseCh chan int64
	newSizeCh chan int64
	resetCh   chan bool
}

// NewBucket returns a Bucket with a given amount of initial free space.
func NewBucket(bucketSize int64) *Bucket {
	b := &Bucket{
		size:      bucketSize,
		acquireCh: make(chan int64),
		releaseCh: make(chan int64, 1),
		newSizeCh: make(chan int64),
		resetCh:   make(chan bool, 1),
	}

	b.startBucket()

	return b
}

// Acquire gets some space from the Bucket.
// This function is blocking, unless the passed context.Context has a deadline which is exceeded
// before the Bucket has a chance to get bucket space.
// If the passed context.Context is marked Done before bucket space is acquired, ctx.Err() is returned.
// If bucket space is acquired, nil is returned.
func (b *Bucket) Acquire(ctx context.Context) error {
	for {
		select {
		case <-b.acquireCh:
			return nil
		case <-ctx.Done():
			ctxErr := ctx.Err()
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return errors.Wrap(ctxErr, "Bucket.Acquire(): context deadline exceeded waiting for open bucket space")
			}

			return errors.Wrap(ctxErr, "Bucket.Acquire(): context error")
		}
	}
}

// Release gives space back to the Bucket.
func (b *Bucket) Release() {
	go func(b *Bucket) { b.releaseCh <- 1 }(b)
}

// Empty tells the goroutine listening on Bucket's various channels that it now has ALL the free space!
func (b *Bucket) Empty() {
	b.resetCh <- true
}

func (b *Bucket) SetSize(newSize int64) {
	b.size = newSize
	b.newSizeCh <- newSize
}

func (b *Bucket) startBucket() {
	go func(b *Bucket) {
		var (
			lastBucketSize = b.size
			available      = b.size
		)

		for {
			select {
			case newSize := <-b.newSizeCh:
				diff := newSize - lastBucketSize
				available += diff

				lastBucketSize = newSize
			case <-b.releaseCh:
				available++
			case <-b.resetCh:
				available = b.size
			default:
				if available == 0 {
					continue
				}

				available--
				b.acquireCh <- 1
			}
		}
	}(b)
}
