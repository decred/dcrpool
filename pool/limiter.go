// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"sync"

	"golang.org/x/time/rate"
)

// Client types.
const (
	APIClient = iota
	PoolClient
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

// addRequestLimiter adds a new client request limiter to the limiter set.
func (r *RateLimiter) addRequestLimiter(ip string, clientType int) (*rate.Limiter, error) {
	var limiter *rate.Limiter
	switch clientType {
	case APIClient:
		limiter = rate.NewLimiter(apiTokenRate, apiBurst)
	case PoolClient:
		limiter = rate.NewLimiter(clientTokenRate, clientBurst)
	default:
		return nil, fmt.Errorf("unknown client type provided: %d", clientType)
	}

	r.mutex.Lock()
	r.limiters[ip] = limiter
	r.mutex.Unlock()

	return limiter, nil
}

// fetchLimiter fetches the request limiter referenced by the provided
// IP address.
func (r *RateLimiter) fetchLimiter(ip string) *rate.Limiter {
	r.mutex.RLock()
	limiter := r.limiters[ip]
	r.mutex.RUnlock()
	return limiter
}

// RemoveLimiter deletes the request limiter associated with the provided ip.
func (r *RateLimiter) removeLimiter(ip string) {
	r.mutex.Lock()
	delete(r.limiters, ip)
	r.mutex.Unlock()
}

// withinLimit asserts that the client referenced by the provided IP
// address is within the limits of the rate limiter, therefore can make
// further requests. If no request limiter is found for the provided IP
// address a new one is created. withinLimit returns false if an unknown
// client type is provided.
func (r *RateLimiter) withinLimit(ip string, clientType int) bool {
	reqLimiter := r.fetchLimiter(ip)
	if reqLimiter == nil {
		// Create a new limiter if the incoming request is from a new client.
		var err error
		reqLimiter, err = r.addRequestLimiter(ip, clientType)
		if err != nil {
			log.Error(err)
			return false
		}
	}
	return reqLimiter.Allow()
}
