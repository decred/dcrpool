package network

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// For a connected client within fifteen (15) seconds three (3) ping
	// responses and a submit work request. The current limiter settings
	// allows for thirty (30) tokens generated every fifteen (15) seconds.
	// This should be enough for the requests of the connected client as
	// well as requests vai the pool API.

	// tokenRate is the token refill rate for the bucket per second.
	tokenRate = 2
	// burst is the maximum token usage allowed per call.
	burst = 3
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

// GetRequestLimiter fetches the request limiter referenced by the provided
// IP address.
func (r *RateLimiter) GetRequestLimiter(ip string) *RequestLimiter {
	r.mutex.RLock()
	limiter := r.limiters[ip]
	r.mutex.RUnlock()
	return limiter
}

// pastTime regresses a base time to a time in the past.
func pastTime(date *time.Time, days time.Duration, hours time.Duration,
	minutes time.Duration, seconds time.Duration) time.Time {
	duration := ((time.Hour * 24) * days) + (time.Hour * hours) +
		(time.Minute * minutes) + (time.Second * seconds)
	pastTime := date.Add(-duration)
	return pastTime
}

// DiscardLimiters iterates through the limiters set and removes all
// limiters that have not received a request in five minutes or more.
func (r *RateLimiter) DiscardLimiters() {
	now := time.Now()
	fiveMinAgo := uint32(pastTime(&now, 0, 0, 5, 0).Unix())
	r.mutex.Lock()
	for ip, limiter := range r.limiters {
		if limiter.lastAllowedRequest < fiveMinAgo {
			delete(r.limiters, ip)
		}
	}
	r.mutex.Unlock()
}

// WithinLimit asserts that the client referenced by the provided IP address is
// within the limits of the rate limiter, therefore can make futher requests.
// If no request limiter is found for the provided IP address a new one is
// created.
func (r *RateLimiter) WithinLimit(ip string) bool {
	reqLimiter := r.GetRequestLimiter(ip)

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
