// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// tokenRate is the token refill rate for the bucket per second. A maximum
	// of 5 requests per second for a pool client that will be mostly submitting
	// work to the pool at a controlled rate is adequate.
	tokenRate = 5

	// burst is the maximum token usage allowed per second.
	burst = 5
)

// RequestLimiter represents a rate limiter for a connecting client. This identifies
// clients by their IP addresses.
type RequestLimiter struct {
	ip                 string
	limiter            *rate.Limiter
	lastAllowedRequest uint32
}

// RateLimiter represents the rate limiting module of the mining pool. It
// identifies clients by their IP address and throttles incoming request if
// necessary when the allocated quota has been exceeded.
type RateLimiter struct {
	mutex    sync.RWMutex
	limiters map[string]*RequestLimiter
}

// NewRateLimiter initializes a rate limiter.
func NewRateLimiter() *RateLimiter {
	RateLimiter := &RateLimiter{
		limiters: make(map[string]*RequestLimiter),
	}
	return RateLimiter
}

// AddRequestLimiter adds a new client request limiter to the limiter set.
func (r *RateLimiter) AddRequestLimiter(ip string) *RequestLimiter {
	limiter := &RequestLimiter{
		ip:                 ip,
		limiter:            rate.NewLimiter(tokenRate, burst),
		lastAllowedRequest: 0,
	}
	r.mutex.Lock()
	r.limiters[ip] = limiter
	r.mutex.Unlock()
	return limiter
}

// GetLimiter fetches the request limiter referenced by the provided
// IP address.
func (r *RateLimiter) GetLimiter(ip string) *RequestLimiter {
	r.mutex.RLock()
	limiter := r.limiters[ip]
	r.mutex.RUnlock()
	return limiter
}

// RemoveLimiter deletes the request limiter associated with the provided ip.
func (r *RateLimiter) RemoveLimiter(ip string) {
	r.mutex.Lock()
	delete(r.limiters, ip)
	r.mutex.Unlock()
}

// WithinLimit asserts that the client referenced by the provided IP address is
// within the limits of the rate limiter, therefore can make further requests.
// If no request limiter is found for the provided IP address a new one is
// created.
func (r *RateLimiter) WithinLimit(ip string) bool {
	reqLimiter := r.GetLimiter(ip)

	// create a new limiter if the incoming request is from a new client.
	if reqLimiter == nil {
		reqLimiter = r.AddRequestLimiter(ip)
	}

	allow := reqLimiter.limiter.Allow()
	if allow {
		// update the last accessed time of the limiter if the incoming request
		// is allowed.
		reqLimiter.lastAllowedRequest = uint32(time.Now().Unix())
	}
	return allow
}
