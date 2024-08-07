// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2024 The Decred developers
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
	// - Modify the version variable below on that branch to:
	//   - Remove the pre-release portion
	//   - Set the build metadata to 'release.local'
	// - Update the version variable below on the master branch to the next
	//   expected version while retaining a pre-release of 'pre'
	//
	// These steps ensure that building from source produces versions that are
	// distinct from reproducible builds that override the version via linker
	// flags.

	// version is the application version per the semantic versioning 2.0.0 spec
	// (https://semver.org/).
	//
	// It is defined as a variable so it can be overridden during the build
	// process with:
	// '-ldflags "-X main.version=fullsemver"'
	// if needed.
	//
	// It MUST be a full semantic version per the semantic versioning spec or
	// the app will panic at runtime.  Of particular note is the pre-release
	// and build metadata portions MUST only contain characters from
	// semanticAlphabet.
	version = "2.0.0-pre"

	// NOTE: The following values are set via init by parsing the above version
	// string.

	// These fields are the individual semantic version components that define
	// the application version.
	major         uint32
	minor         uint32
	patch         uint32
	preRelease    string
	buildMetadata string
)

func init() {
	parsedSemVer, err := semver.Parse(version)
	if err != nil {
		panic(err)
	}
	major = parsedSemVer.Major
	minor = parsedSemVer.Minor
	patch = parsedSemVer.Patch
	preRelease = parsedSemVer.PreRelease
	buildMetadata = parsedSemVer.BuildMetadata
	if buildMetadata == "" {
		buildMetadata = vcsCommitID()
		if buildMetadata != "" {
			version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
			if preRelease != "" {
				version += "-" + preRelease
			}
			version += "+" + buildMetadata
		}
	}
}
