// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"

	"github.com/decred/dcrpool/internal/semver"
)

// These variables define the application version and follow the semantic
// versioning 2.0.0 spec (https://semver.org/).
var (
	// Note for maintainers:
	//
	// The expected process for setting the version in releases is as follows:
	// - Create a release branch of the form 'release-vMAJOR.MINOR'
	// - Modify the Version variable below on that branch to:
	//   - Remove the pre-release portion
	//   - Set the build metadata to 'release.local'
	// - Update the Version variable below on the master branch to the next
	//   expected version while retaining a pre-release of 'pre'
	//
	// These steps ensure that building from source produces versions that are
	// distinct from reproducible builds that override the Version via linker
	// flags.

	// Version is the application version per the semantic versioning 2.0.0 spec
	// (https://semver.org/).
	//
	// It is defined as a variable so it can be overridden during the build
	// process with:
	// '-ldflags "-X main.Version=fullsemver"'
	// if needed.
	//
	// It MUST be a full semantic version per the semantic versioning spec or
	// the app will panic at runtime.  Of particular note is the pre-release
	// and build metadata portions MUST only contain characters from
	// semanticAlphabet.
	Version = "1.3.0-pre"

	// NOTE: The following values are set via init by parsing the above Version
	// string.

	// These fields are the individual semantic version components that define
	// the application version.
	Major         uint32
	Minor         uint32
	Patch         uint32
	PreRelease    string
	BuildMetadata string
)

func init() {
	parsedSemVer, err := semver.Parse(Version)
	if err != nil {
		panic(err)
	}
	Major = parsedSemVer.Major
	Minor = parsedSemVer.Minor
	Patch = parsedSemVer.Patch
	PreRelease = parsedSemVer.PreRelease
	BuildMetadata = parsedSemVer.BuildMetadata
	if BuildMetadata == "" {
		BuildMetadata = vcsCommitID()
		if BuildMetadata != "" {
			Version = fmt.Sprintf("%d.%d.%d", Major, Minor, Patch)
			if PreRelease != "" {
				Version += "-" + PreRelease
			}
			Version += "+" + BuildMetadata
		}
	}
}
