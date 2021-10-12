# ratelimit

A very hacky rate limiter for Go programs. I'm aware that it's probably a hacky and/or bad Semaphore implementation.

It is not recommended to use this in production. Ever. Unless you really want to, I guess.

## bucket

I have exactly zero idea how [bucket](bucket.go) works. If someone can explain it to me, that would be great.

## Installation

`go get -d github.com/stoicturtle/ratelimit`

## Usage

```go
package main

import (
	"net/http"
	"time"
	
	"github.com/stoicturtle/ratelimit"
)

func fnToRateLimit() error {
	_, err := http.Get("https://www.google.com")
	
	return err
}

func main() {
	const (
		numCalls               = 3000
		callsPerInterval int64 = 500
		callInterval           = time.Second
	)
	
	r := ratelimit.NewRateLimiter(callsPerInterval, callInterval)
	
	for i := 0; i < numCalls; i++ {
		// wrap the desired function in a zero-param function which only returns an error
		f := func() error {
			return fnToRateLimit()
		}

		 // one might want to actually check return errors here. 
		_ = r.RateLimit(f) 
	}
}
```