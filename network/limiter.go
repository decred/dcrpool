// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"net/http"
	"sync"
	"time"

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
	apiTokenRate = 1

	// apiBurst is the maximum token usage allowed per second,
	// for api clients.
	apiBurst = 1

	// apiClient represents an api client.
	APIClient = "api"

	// poolClient represents a pool client.
	PoolClient = "pool"
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
func (r *RateLimiter) AddRequestLimiter(ip string, clientType string) *RequestLimiter {
	var limiter *RequestLimiter
	if clientType == APIClient {
		limiter = &RequestLimiter{
			ip:                 ip,
			limiter:            rate.NewLimiter(apiTokenRate, apiBurst),
			lastAllowedRequest: 0,
		}
	}

	if clientType == PoolClient {
		limiter = &RequestLimiter{
			ip:                 ip,
			limiter:            rate.NewLimiter(clientTokenRate, clientBurst),
			lastAllowedRequest: 0,
		}
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

	allow := reqLimiter.limiter.Allow()
	if allow {
		// update the last accessed time of the limiter if the incoming request
		// is allowed.
		reqLimiter.lastAllowedRequest = uint32(time.Now().Unix())
	}
	return allow
}

// LimiterMiddleware wraps the the request limit logic as request middleware.
func (r *RateLimiter) LimiterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		accepted := r.WithinLimit(req.RemoteAddr, APIClient)
		if accepted {
			next.ServeHTTP(w, req)
		}
	})
}
