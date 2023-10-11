// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"testing"
)

// TestNewUserAgentRE ensures the mining client user agent regular-expression
// matching logic works as intended.
func TestNewUserAgentRE(t *testing.T) {
	// perRETest describes a test to run against the same regular expression.
	type perRETest struct {
		clientUA  string // user agent string to test
		wantMatch bool   // expected match result
	}

	// makePerRETests returns a series of tests for a variety of client UAs that
	// are generated based on the provided parameters to help ensure the exact
	// semantics that each test intends to test are actually what is being
	// tested.
	makePerRETests := func(client string, major, minor uint32) []perRETest {
		p := fmt.Sprintf
		pcmm := func(format string, a ...interface{}) string {
			params := make([]interface{}, 0, len(a)+3)
			params = append(params, client)
			params = append(params, major)
			params = append(params, minor)
			params = append(params, a...)
			return p(format, params...)
		}
		return []perRETest{
			// All patch versions including multi digit are allowed.
			{pcmm("%s/%d.%d.0"), true},
			{pcmm("%s/%d.%d.1"), true},
			{pcmm("%s/%d.%d.2"), true},
			{pcmm("%s/%d.%d.3"), true},
			{pcmm("%s/%d.%d.4"), true},
			{pcmm("%s/%d.%d.5"), true},
			{pcmm("%s/%d.%d.6"), true},
			{pcmm("%s/%d.%d.7"), true},
			{pcmm("%s/%d.%d.8"), true},
			{pcmm("%s/%d.%d.9"), true},
			{pcmm("%s/%d.%d.10"), true},

			// All valid prerelease and build metadata combinations are allowed.
			{pcmm("%s/%d.%d.0-prerelease+meta"), true},
			{pcmm("%s/%d.%d.0+meta"), true},
			{pcmm("%s/%d.%d.0+meta-valid"), true},
			{pcmm("%s/%d.%d.0-alpha"), true},
			{pcmm("%s/%d.%d.0-beta"), true},
			{pcmm("%s/%d.%d.0-alpha.beta"), true},
			{pcmm("%s/%d.%d.0-alpha.beta.1"), true},
			{pcmm("%s/%d.%d.0-alpha.1"), true},
			{pcmm("%s/%d.%d.0-alpha0.valid"), true},
			{pcmm("%s/%d.%d.0-alpha.0valid"), true},
			{pcmm("%s/%d.%d.0-alpha.a.b-c-somethinglong+build.1-aef.1-its-okay"), true},
			{pcmm("%s/%d.%d.0-rc.1+build.1"), true},
			{pcmm("%s/%d.%d.0-rc.1+build.123"), true},
			{pcmm("%s/%d.%d.1-beta"), true},
			{pcmm("%s/%d.%d.10-DEV-SNAPSHOT"), true},
			{pcmm("%s/%d.%d.100-SNAPSHOT-123"), true},
			{pcmm("%s/%d.%d.0+build.1848"), true},
			{pcmm("%s/%d.%d.1-alpha.1227"), true},
			{pcmm("%s/%d.%d.0-alpha+beta"), true},
			{pcmm("%s/%d.%d.0-----RC-SNAPSHOT.12.9.1--.12+788"), true},
			{pcmm("%s/%d.%d.3----R-S.12.9.1--.12+meta"), true},
			{pcmm("%s/%d.%d.0----RC-SNAPSHOT.12.9.1--.12"), true},
			{pcmm("%s/%d.%d.0+0.build.1-rc.10000aaa-kk-0.1"), true},
			{pcmm("%s/%d.%d.0-0A.is.legal"), true},

			// New minor revisions are not allowed.
			{p("%s/%d.%d.0", client, major, minor+1), false},
			{p("%s/%d.%d.0", client, major, minor+2), false},
			{p("%s/%d.%d.0", client, major, minor+10), false},

			// New major versions are not allowed.
			{p("%s/%d.0.0", client, major+1), false},
			{p("%s/%d.0.0", client, major+2), false},
			{p("%s/%d.0.0", client, major+10), false},

			// Prefixes not allowed.
			{pcmm(" %s/%d.%d.0"), false},
			{pcmm("a%s/%d.%d.0"), false},
			{pcmm("1%s/%d.%d.0"), false},

			// Invalid semantic versions not allowed.
			{p("%s/%d", client, major), false},
			{pcmm("%s/%d.%d"), false},
			{pcmm("%s/%d.%d.0-0123"), false},
			{pcmm("%s/%d.%d.0-0123.0123"), false},
			{pcmm("%s/%d.%d.1+.123"), false},
			{pcmm("%s/%d.%d+invalid"), false},
			{pcmm("%s/%d.%d-invalid"), false},
			{pcmm("%s/%d.%d-invalid+invalid"), false},
			{pcmm("%s/%d.%d.0-alpha_beta"), false},
			{pcmm("%s/%d.%d.0-alpha.."), false},
			{pcmm("%s/%d.%d.0-alpha..1"), false},
			{pcmm("%s/%d.%d.0-alpha...1"), false},
			{pcmm("%s/%d.%d.0-alpha....1"), false},
			{pcmm("%s/%d.%d.0-alpha.....1"), false},
			{pcmm("%s/%d.%d.0-alpha......1"), false},
			{pcmm("%s/%d.%d.0-alpha.......1"), false},
			{pcmm("%s/%d.%d.0-alpha..1"), false},
			{pcmm("%s/0%d.%d.0"), false},
			{pcmm("%s/%d.0%d.0"), false},
			{pcmm("%s/%d.%d.00"), false},
			{pcmm("%s/%d.%d.0.DEV"), false},
			{pcmm("%s/%d.%d-SNAPSHOT"), false},
			{pcmm("%s/%d.%d.31.2.3----RC-SNAPSHOT.12.09.1--..12+788"), false},
			{pcmm("%s/%d.%d-RC-SNAPSHOT"), false},
			{pcmm("%s/-%d.%d.3-gamme+b7718"), false},
			{p("%s/+justmeta", client), false},
			{pcmm("%s/%d.%d.7+meta+meta"), false},
			{pcmm("%s/%d.%d.7-whatever+meta+meta"), false},
		}
	}

	tests := []struct {
		name       string // test description
		clientName string // required client name for regexp
		major      uint32 // required major ver of the client for regexp
		minor      uint32 // required minor ver of the client for regexp
	}{{
		name:       "cpuminer/1.0.x",
		clientName: "cpuminer",
		major:      1,
		minor:      0,
	}, {
		name:       "cpuminer/1.1.x",
		clientName: "cpuminer",
		major:      1,
		minor:      1,
	}, {
		name:       "cpuminer/2.4.x",
		clientName: "cpuminer",
		major:      2,
		minor:      4,
	}, {
		name:       "otherminer/10.17.x",
		clientName: "otherminer",
		major:      10,
		minor:      17,
	}}

	for _, test := range tests {
		// Create the compiled regular expression as well as client UAs and
		// expected results.
		re := newUserAgentRE(test.clientName, test.major, test.minor)
		perRETests := makePerRETests(test.clientName, test.major, test.minor)

		// Ensure all of the client UAs produce the expected match results.
		for _, subTest := range perRETests {
			gotMatch := re.MatchString(subTest.clientUA)
			if gotMatch != subTest.wantMatch {
				t.Errorf("%s: (ua: %q): unexpected match result -- got %v, want %v",
					test.name, subTest.clientUA, gotMatch, subTest.wantMatch)
				continue
			}
		}
	}
}
