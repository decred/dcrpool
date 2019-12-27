package pool

import "testing"

func testLimiter(t *testing.T) {
	limiter := NewRateLimiter()
	apiLimiterIP := "127.0.0.1"

	// Ensure the api limiter is within range.
	if !limiter.withinLimit(apiLimiterIP, APIClient) {
		t.Fatal("expected limiter to be within limit")
	}

	// Exhaust the api limiter range.
	for limiter.withinLimit(apiLimiterIP, APIClient) {
		continue
	}

	// Fetch the api limiter
	lmt := limiter.fetchLimiter(apiLimiterIP)
	if lmt == nil {
		t.Fatalf("expected a non-nil limiter")
	}

	poolLimiterIP := "0.0.0.0"

	// Ensure the api limiter is within range.
	if !limiter.withinLimit(poolLimiterIP, PoolClient) {
		t.Fatal("expected limiter to be within limit")
	}

	// Exhaust the pool limiter range.
	for limiter.withinLimit(poolLimiterIP, PoolClient) {
		continue
	}

	// Fetch the pool limiter.
	lmt = limiter.fetchLimiter(apiLimiterIP)
	if lmt == nil {
		t.Fatalf("expected a non-nil limiter")
	}

	// Remove limiters.
	limiter.removeLimiter(apiLimiterIP)
	limiter.removeLimiter(poolLimiterIP)

	// Ensure the limiters have been removed.
	lmt = limiter.fetchLimiter(apiLimiterIP)
	if lmt != nil {
		t.Fatalf("expected a nil limiter")
	}
}
