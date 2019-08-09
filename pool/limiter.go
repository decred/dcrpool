// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"sync"

	"golang.org/x/time/rate"
)

const (
	// clientTokenRate is the token refill rate for the client request bucket,
	// per second. A maximum of 5 requests per second for a pool client that
	// will be mostly submitting work to the pool at a controlled rate is
	// adequate.
	clientTokenRate = 5

	// clientBurst is the maximum token usage allowed per second,
	// for pool clients.
	clientBurst = 5

	// apiTokenRate is the token refill rate for the api request bucket,
	// per second.
	apiTokenRate = 3

	// apiBurst is the maximum token usage allowed per second,
	// for api clients.
	apiBurst = 3

	// apiClient represents an api client.
	APIClient = "api"

	// poolClient represents a pool client.
	PoolClient = "pool"
)

// RateLimiter keeps connected clients within their allocated request rates.
type RateLimiter struct {
	mutex    sync.RWMutex
	limiters map[string]*rate.Limiter
}

// NewRateLimiter initializes a rate limiter.
func NewRateLimiter() *RateLimiter {
	limiters := &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
	return limiters
}

// AddRequestLimiter adds a new client request limiter to the limiter set.
func (r *RateLimiter) AddRequestLimiter(ip string, clientType string) *rate.Limiter {
	var limiter *rate.Limiter
	switch clientType {
	case APIClient:
		limiter = rate.NewLimiter(apiTokenRate, apiBurst)
	case PoolClient:
		limiter = rate.NewLimiter(clientTokenRate, clientBurst)
	default:
		log.Errorf("unknown client type provided: %s", clientType)
		return nil
	}

	r.mutex.Lock()
	r.limiters[ip] = limiter
	r.mutex.Unlock()

	return limiter
}

// GetLimiter fetches the request limiter referenced by the provided
// IP address.
func (r *RateLimiter) GetLimiter(ip string) *rate.Limiter {
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

// WithinLimit asserts that the client referenced by the provided IP
// address is within the limits of the rate limiter, therefore can make
// further requests. If no request limiter is found for the provided IP
// address a new one is created.
func (r *RateLimiter) WithinLimit(ip string, clientType string) bool {
	reqLimiter := r.GetLimiter(ip)

	// create a new limiter if the incoming request is from a new client.
	if reqLimiter == nil {
		reqLimiter = r.AddRequestLimiter(ip, clientType)
	}

	return reqLimiter.Allow()
}
