// Copyright (c) 2015-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package semver parses semantic versioning 2.0.0 version strings.
package semver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// semanticAlphabet defines the allowed characters for the pre-release and
	// build metadata portions of a semantic version string.
	semanticAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-."
)

// semverRE is a regular expression used to parse a semantic version string into
// its constituent parts.
var semverRE = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*` +
	`[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// parseUint32 converts the passed string to an unsigned integer or returns an
// error if it is invalid.
func parseUint32(s string, fieldName string) (uint32, error) {
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("malformed semver %s: %w", fieldName, err)
	}
	return uint32(val), err
}

// checkSemString returns an error if the passed string contains characters that
// are not in the provided alphabet.
func checkSemString(s, alphabet, fieldName string) error {
	for _, r := range s {
		if !strings.ContainsRune(alphabet, r) {
			return fmt.Errorf("malformed semver %s: %q invalid", fieldName, r)
		}
	}
	return nil
}

// ParsedSemVer houses the individual components of a parsed semantic versioning
// 2.0.0 version string.
type ParsedSemVer struct {
	Major         uint32
	Minor         uint32
	Patch         uint32
	PreRelease    string
	BuildMetadata string
}

// Parse attempts to parse the various semver components from the provided
// version string.
func Parse(s string) (*ParsedSemVer, error) {
	// Parse the various semver component from the version string via a regular
	// expression.
	m := semverRE.FindStringSubmatch(s)
	if m == nil {
		err := fmt.Errorf("malformed version string %q: does not conform to "+
			"semver specification", s)
		return nil, err
	}

	major, err := parseUint32(m[1], "major")
	if err != nil {
		return nil, err
	}

	minor, err := parseUint32(m[2], "minor")
	if err != nil {
		return nil, err
	}

	patch, err := parseUint32(m[3], "patch")
	if err != nil {
		return nil, err
	}

	preRel := m[4]
	err = checkSemString(preRel, semanticAlphabet, "pre-release")
	if err != nil {
		return nil, err
	}

	build := m[5]
	err = checkSemString(build, semanticAlphabet, "buildmetadata")
	if err != nil {
		return nil, err
	}

	return &ParsedSemVer{
		Major:         major,
		Minor:         minor,
		Patch:         patch,
		PreRelease:    preRel,
		BuildMetadata: build,
	}, nil
}
