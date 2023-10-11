// Copyright (c) 2020-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"regexp"

	errs "github.com/decred/dcrpool/errors"
)

// newUserAgentRE returns a compiled regular expression that matches a user
// agent with the provided client name, major version, and minor version as well
// as any patch, pre-release, and build metadata suffix that are valid per the
// semantic versioning 2.0.0 spec.
//
// For reference, user agents are expected to be of the form "name/version"
// where the name is a string and the version follows the semantic versioning
// 2.0.0 spec.
func newUserAgentRE(clientName string, clientMajor, clientMinor uint32) *regexp.Regexp {
	// semverBuildAndMetadataSuffixRE is a regular expression to match the
	// optional pre-release and build metadata portions of a semantic version
	// 2.0 string.
	const semverBuildAndMetadataSuffixRE = `(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-]` +
		`[0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?`

	return regexp.MustCompile(fmt.Sprintf(`^%s\/%d\.%d\.(0|[1-9]\d*)%s$`,
		clientName, clientMajor, clientMinor, semverBuildAndMetadataSuffixRE))
}

var (
	// These regular expressions are used to identify the expected mining
	// clients by the user agents in their mining.subscribe requests.
	cpuRE = newUserAgentRE("cpuminer", 1, 0)
	nhRE  = newUserAgentRE("NiceHash", 1, 0)

	// miningClients maps regular expressions to the supported mining client IDs
	// for all user agents that match the regular expression.
	miningClients = map[*regexp.Regexp][]string{
		cpuRE: {CPU},
		nhRE:  {NiceHashValidator},
	}
)

// identifyMiningClients returns the possible mining client IDs for a given user agent
// or an error when the user agent is not supported.
func identifyMiningClients(userAgent string) ([]string, error) {
	for re, clients := range miningClients {
		if re.MatchString(userAgent) {
			return clients, nil
		}
	}

	msg := fmt.Sprintf("connected miner with id %s is unsupported", userAgent)
	return nil, errs.PoolError(errs.MinerUnknown, msg)
}
