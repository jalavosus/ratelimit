package ratelimit

import (
	"sync"
	"sync/atomic"
)

const (
	resetStateNeedsReset int64 = 1
	resetStateCleared    int64 = 0
)

type bucket struct {
	maxRequests   int64
	requestBucket *int64
	mux           *sync.RWMutex
	resetState    *int64
}

func newBucket(bucketSize int64) *bucket {
	nr := new(int64)
	*nr = resetStateCleared

	return &bucket{
		maxRequests:   bucketSize,
		requestBucket: new(int64),
		mux:           new(sync.RWMutex),
		resetState:    nr,
	}
}

func (b *bucket) addToBucket() {
	bucketCount := b.getBucketCount()
	if bucketCount >= b.maxRequests {
		b.setNeedsReset()
		b.waitReset()
	}

	atomic.AddInt64(b.requestBucket, 1)
}

func (b *bucket) removeFromBucket() {
	bucketCount := b.getBucketCount()
	if bucketCount == 0 {
		b.setResetStateCleared()
		return
	}

	atomic.AddInt64(b.requestBucket, -1)
}

func (b *bucket) emptyBucket() {
	atomic.StoreInt64(b.requestBucket, 0)
}

func (b *bucket) setNeedsReset() {
	atomic.CompareAndSwapInt64(b.resetState, b.getResetState(), resetStateNeedsReset)
}

func (b *bucket) waitReset() {
	for b.getResetState() == resetStateNeedsReset {
	}
}

func (b *bucket) setResetStateCleared() {
	atomic.CompareAndSwapInt64(b.resetState, b.getResetState(), resetStateCleared)
}

func (b *bucket) getResetState() int64 {
	return atomic.LoadInt64(b.resetState)
}

func (b *bucket) setMaxRequests(n int64) {
	b.maxRequests = n
}

func (b *bucket) getBucketCount() int64 {
	return atomic.LoadInt64(b.requestBucket)
}
