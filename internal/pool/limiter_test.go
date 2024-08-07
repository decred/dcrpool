// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import "testing"

func TestLimiter(t *testing.T) {
	limiter := NewRateLimiter()
	guiLimiterIP := "127.0.0.1"

	// Ensure the gui limiter is within range.
	if !limiter.withinLimit(guiLimiterIP, GUIClient) {
		t.Fatal("expected limiter to be within limit")
	}

	// Exhaust the gui limiter range.
	for limiter.withinLimit(guiLimiterIP, GUIClient) {
		continue
	}

	// Fetch the gui limiter.
	lmt := limiter.fetchLimiter(guiLimiterIP)
	if lmt == nil {
		t.Fatalf("expected a non-nil limiter")
	}

	poolLimiterIP := "0.0.0.0"

	// Ensure the pool limiter is within range.
	if !limiter.withinLimit(poolLimiterIP, PoolClient) {
		t.Fatal("expected limiter to be within limit")
	}

	// Exhaust the pool limiter range.
	for limiter.withinLimit(poolLimiterIP, PoolClient) {
		continue
	}

	// Fetch the pool limiter.
	lmt = limiter.fetchLimiter(poolLimiterIP)
	if lmt == nil {
		t.Fatalf("expected a non-nil limiter")
	}

	unknownIP := "8.8.8.8"

	// Ensure the limiter does not create a rate limiter
	// for an unknown client type.
	if limiter.withinLimit(unknownIP, 10) {
		t.Fatal("expected limiter to not be within limit")
	}

	// Ensure a limiter was not created for the unknown client type.
	lmt = limiter.fetchLimiter(unknownIP)
	if lmt != nil {
		t.Fatalf("expected a nil limiter")
	}

	// Remove limiters.
	limiter.removeLimiter(guiLimiterIP)
	limiter.removeLimiter(poolLimiterIP)

	// Ensure the limiters have been removed.
	lmt = limiter.fetchLimiter(guiLimiterIP)
	if lmt != nil {
		t.Fatalf("expected a nil limiter")
	}
}
