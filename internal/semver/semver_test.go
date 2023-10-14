// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package semver

import "testing"

// TestParse ensures the semantic versioning 2.0.0 parsing logic works as
// intended.
func TestParse(t *testing.T) {
	tests := []struct {
		version           string // version string to parse
		wantErr           bool   // whether or not an error is expected
		wantMajor         uint32 // expected major version
		wantMinor         uint32 // expected minor version
		wantPatch         uint32 // expected patch version
		wantPreRelease    string // expected pre-release string
		wantBuildMetadata string // expected build metadata string
	}{{
		version:   "0.0.4",
		wantMajor: 0,
		wantMinor: 0,
		wantPatch: 4,
	}, {
		version:   "1.2.3",
		wantMajor: 1,
		wantMinor: 2,
		wantPatch: 3,
	}, {
		version:   "10.20.30",
		wantMajor: 10,
		wantMinor: 20,
		wantPatch: 30,
	}, {
		version:           "1.1.2-prerelease+meta",
		wantMajor:         1,
		wantMinor:         1,
		wantPatch:         2,
		wantPreRelease:    "prerelease",
		wantBuildMetadata: "meta",
	}, {
		version:           "1.1.2+meta",
		wantMajor:         1,
		wantMinor:         1,
		wantPatch:         2,
		wantBuildMetadata: "meta",
	}, {
		version:           "1.1.2+meta-valid",
		wantMajor:         1,
		wantMinor:         1,
		wantPatch:         2,
		wantBuildMetadata: "meta-valid",
	}, {
		version:        "1.0.0-alpha",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "alpha",
	}, {
		version:        "1.0.0-beta",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "beta",
	}, {
		version:        "1.0.0-alpha.beta",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "alpha.beta",
	}, {
		version:        "1.0.0-alpha.beta.1",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "alpha.beta.1",
	}, {
		version:        "1.0.0-alpha.1",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "alpha.1",
	}, {
		version:        "1.0.0-alpha0.valid",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "alpha0.valid",
	}, {
		version:        "1.0.0-alpha.0valid",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "alpha.0valid",
	}, {
		version:           "1.0.0-alpha-a.b-c-somethinglong+build.1-aef.1-its-okay",
		wantMajor:         1,
		wantMinor:         0,
		wantPatch:         0,
		wantPreRelease:    "alpha-a.b-c-somethinglong",
		wantBuildMetadata: "build.1-aef.1-its-okay",
	}, {
		version:           "1.0.0-rc.1+build.1",
		wantMajor:         1,
		wantMinor:         0,
		wantPatch:         0,
		wantPreRelease:    "rc.1",
		wantBuildMetadata: "build.1",
	}, {
		version:           "2.0.0-rc.1+build.123",
		wantMajor:         2,
		wantMinor:         0,
		wantPatch:         0,
		wantPreRelease:    "rc.1",
		wantBuildMetadata: "build.123",
	}, {
		version:        "1.2.3-beta",
		wantMajor:      1,
		wantMinor:      2,
		wantPatch:      3,
		wantPreRelease: "beta",
	}, {
		version:        "10.2.3-DEV-SNAPSHOT",
		wantMajor:      10,
		wantMinor:      2,
		wantPatch:      3,
		wantPreRelease: "DEV-SNAPSHOT",
	}, {
		version:        "1.2.3-SNAPSHOT-123",
		wantMajor:      1,
		wantMinor:      2,
		wantPatch:      3,
		wantPreRelease: "SNAPSHOT-123",
	}, {
		version:   "1.0.0",
		wantMajor: 1,
		wantMinor: 0,
		wantPatch: 0,
	}, {
		version:   "2.0.0",
		wantMajor: 2,
		wantMinor: 0,
		wantPatch: 0,
	}, {
		version:   "1.1.7",
		wantMajor: 1,
		wantMinor: 1,
		wantPatch: 7,
	}, {
		version:           "2.0.0+build.1848",
		wantMajor:         2,
		wantMinor:         0,
		wantPatch:         0,
		wantBuildMetadata: "build.1848",
	}, {
		version:        "2.0.1-alpha.1227",
		wantMajor:      2,
		wantMinor:      0,
		wantPatch:      1,
		wantPreRelease: "alpha.1227",
	}, {
		version:           "1.0.0-alpha+beta",
		wantMajor:         1,
		wantMinor:         0,
		wantPatch:         0,
		wantPreRelease:    "alpha",
		wantBuildMetadata: "beta",
	}, {
		version:           "1.2.3----RC-SNAPSHOT.12.9.1--.12+788",
		wantMajor:         1,
		wantMinor:         2,
		wantPatch:         3,
		wantPreRelease:    "---RC-SNAPSHOT.12.9.1--.12",
		wantBuildMetadata: "788",
	}, {
		version:           "1.2.3----R-S.12.9.1--.12+meta",
		wantMajor:         1,
		wantMinor:         2,
		wantPatch:         3,
		wantPreRelease:    "---R-S.12.9.1--.12",
		wantBuildMetadata: "meta",
	}, {
		version:        "1.2.3----RC-SNAPSHOT.12.9.1--.12",
		wantMajor:      1,
		wantMinor:      2,
		wantPatch:      3,
		wantPreRelease: "---RC-SNAPSHOT.12.9.1--.12",
	}, {
		version:           "1.0.0+0.build.1-rc.10000aaa-kk-0.1",
		wantMajor:         1,
		wantMinor:         0,
		wantPatch:         0,
		wantBuildMetadata: "0.build.1-rc.10000aaa-kk-0.1",
	}, {
		version:        "1.0.0-0A.is.legal",
		wantMajor:      1,
		wantMinor:      0,
		wantPatch:      0,
		wantPreRelease: "0A.is.legal",
	}, {
		version: "1",
		wantErr: true,
	}, {
		version: "1.2",
		wantErr: true,
	}, {
		version: "1.2.3-0123",
		wantErr: true,
	}, {
		version: "1.2.3-0123.0123",
		wantErr: true,
	}, {
		version: "1.1.2+.123",
		wantErr: true,
	}, {
		version: "+invalid",
		wantErr: true,
	}, {
		version: "-invalid",
		wantErr: true,
	}, {
		version: "-invalid+invalid",
		wantErr: true,
	}, {
		version: "-invalid.01",
		wantErr: true,
	}, {
		version: "alpha",
		wantErr: true,
	}, {
		version: "alpha.beta",
		wantErr: true,
	}, {
		version: "alpha.beta.1",
		wantErr: true,
	}, {
		version: "alpha.1",
		wantErr: true,
	}, {
		version: "alpha+beta",
		wantErr: true,
	}, {
		version: "alpha_beta",
		wantErr: true,
	}, {
		version: "alpha.",
		wantErr: true,
	}, {
		version: "alpha..",
		wantErr: true,
	}, {
		version: "beta",
		wantErr: true,
	}, {
		version: "1.0.0-alpha_beta",
		wantErr: true,
	}, {
		version: "-alpha.",
		wantErr: true,
	}, {
		version: "1.0.0-alpha..",
		wantErr: true,
	}, {
		version: "1.0.0-alpha..1",
		wantErr: true,
	}, {
		version: "1.0.0-alpha...1",
		wantErr: true,
	}, {
		version: "1.0.0-alpha....1",
		wantErr: true,
	}, {
		version: "1.0.0-alpha.....1",
		wantErr: true,
	}, {
		version: "1.0.0-alpha......1",
		wantErr: true,
	}, {
		version: "1.0.0-alpha.......1",
		wantErr: true,
	}, {
		version: "01.1.1",
		wantErr: true,
	}, {
		version: "1.01.1",
		wantErr: true,
	}, {
		version: "1.1.01",
		wantErr: true,
	}, {
		version: "1.2",
		wantErr: true,
	}, {
		version: "1.2.3.DEV",
		wantErr: true,
	}, {
		version: "1.2-SNAPSHOT",
		wantErr: true,
	}, {
		version: "1.2.31.2.3----RC-SNAPSHOT.12.09.1--..12+788",
		wantErr: true,
	}, {
		version: "1.2-RC-SNAPSHOT",
		wantErr: true,
	}, {
		version: "-1.0.3-gamma+b7718",
		wantErr: true,
	}, {
		version: "+justmeta",
		wantErr: true,
	}, {
		version: "9.8.7+meta+meta",
		wantErr: true,
	}, {
		version: "9.8.7-whatever+meta+meta",
		wantErr: true,
	}}

	for _, test := range tests {
		// Parse the test version string and ensure the error result is as
		// expected.
		parsed, err := Parse(test.version)
		if test.wantErr != (err != nil) {
			t.Errorf("%q: unexpected error result -- got %v", test.version, err)
			continue
		}
		if err != nil {
			continue
		}

		// Ensure all parsed values matches their expected values.
		if parsed.Major != test.wantMajor {
			t.Errorf("%q: unexpected major -- got %d, want %d", test.version,
				parsed.Major, test.wantMajor)
			continue
		}
		if parsed.Minor != test.wantMinor {
			t.Errorf("%q: unexpected minor -- got %d, want %d", test.version,
				parsed.Minor, test.wantMinor)
			continue
		}
		if parsed.Patch != test.wantPatch {
			t.Errorf("%q: unexpected patch -- got %d, want %d", test.version,
				parsed.Patch, test.wantPatch)
			continue
		}
		if parsed.PreRelease != test.wantPreRelease {
			t.Errorf("%q: unexpected prerelease -- got %s, want %s",
				test.version, parsed.PreRelease, test.wantPreRelease)
			continue
		}
		if parsed.BuildMetadata != test.wantBuildMetadata {
			t.Errorf("%q: unexpected build metadata -- got %s, want %s",
				test.version, parsed.BuildMetadata, test.wantBuildMetadata)
			continue
		}
	}
}
