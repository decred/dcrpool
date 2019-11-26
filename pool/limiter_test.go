package pool

import "testing"

func testLimiter(t *testing.T) {
	limiter := NewRateLimiter()
	apiLimiterIP := "127.0.0.1"

	// Ensure the api limiter is within range.
	if !limiter.WithinLimit(apiLimiterIP, APIClient) {
		t.Fatal("expected limiter to be within limit")
	}

	// Exhaust the api limiter range.
	for limiter.WithinLimit(apiLimiterIP, APIClient) {
		continue
	}

	// Fetch the api limiter
	lmt := limiter.GetLimiter(apiLimiterIP)
	if lmt == nil {
		t.Fatalf("expected a non-nil limiter")
	}

	poolLimiterIP := "0.0.0.0"

	// Ensure the api limiter is within range.
	if !limiter.WithinLimit(poolLimiterIP, PoolClient) {
		t.Fatal("expected limiter to be within limit")
	}

	// Exhaust the pool limiter range.
	for limiter.WithinLimit(poolLimiterIP, PoolClient) {
		continue
	}

	// Fetch the pool limiter.
	lmt = limiter.GetLimiter(apiLimiterIP)
	if lmt == nil {
		t.Fatalf("expected a non-nil limiter")
	}

	// Remove limiters.
	limiter.RemoveLimiter(apiLimiterIP)
	limiter.RemoveLimiter(poolLimiterIP)

	// Ensure the limiters have been removed.
	lmt = limiter.GetLimiter(apiLimiterIP)
	if lmt != nil {
		t.Fatalf("expected a nil limiter")
	}
}
