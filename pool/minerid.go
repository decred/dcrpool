// Copyright (c) 2020-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"strings"

	errs "github.com/decred/dcrpool/errors"
	"github.com/decred/dcrpool/internal/semver"
)

var (
	// supportedClientUserAgents maps user agents that match a pattern to the
	// supported mining client IDs.
	supportedClientUserAgents = []userAgentToClientsFilter{
		{matchesUserAgentMaxMinor("cpuminer", 1, 0), []string{CPU}},
		{matchesUserAgentMaxMinor("decred-gominer", 2, 1), []string{Gominer}},
		{matchesUserAgentMaxMinor("NiceHash", 1, 0), []string{NiceHashValidator}},
	}
)

// parsedUserAgent houses the individual components of a parsed user agent
// string.
type parsedUserAgent struct {
	semver.ParsedSemVer
	clientName string
}

// parseUserAgent attempts to parse a user agent into its constituent parts and
// returns whether or not it was successful.
func parseUserAgent(userAgent string) (*parsedUserAgent, bool) {
	// Attempt to split the user agent into the client name and client version
	// parts.
	parts := strings.SplitN(userAgent, "/", 2)
	if len(parts) != 2 {
		return nil, false
	}
	clientName := parts[0]
	clientVer := parts[1]

	// Attempt to parse the client version into the constituent semantic version
	// 2.0.0 parts.
	parsedSemVer, err := semver.Parse(clientVer)
	if err != nil {
		return nil, false
	}

	return &parsedUserAgent{
		ParsedSemVer: *parsedSemVer,
		clientName:   clientName,
	}, true
}

// userAgentMatchFn defines a match function that takes a parsed user agent and
// returns whether or not it matches some criteria.
type userAgentMatchFn func(*parsedUserAgent) bool

// userAgentToClientsFilter houses a function to use for matching a user agent
// along with the clients all user agents that match are mapped to.
type userAgentToClientsFilter struct {
	matchFn userAgentMatchFn
	clients []string
}

// matchesUserAgentMaxMinor returns a user agent matching function that returns
// true in the case the user agent matches the provided client name and major
// version and its minor version is less than or equal to the provided minor
// version.
func matchesUserAgentMaxMinor(clientName string, requiredMajor, maxMinor uint32) userAgentMatchFn {
	return func(parsedUA *parsedUserAgent) bool {
		return parsedUA.clientName == clientName &&
			parsedUA.Major == requiredMajor &&
			parsedUA.Minor <= maxMinor
	}
}

// identifyMiningClients returns the possible mining client IDs for a given user
// agent or an error when the user agent is not supported.
func identifyMiningClients(userAgent string) ([]string, error) {
	parsedUA, ok := parseUserAgent(userAgent)
	if ok {
		for _, filter := range supportedClientUserAgents {
			if filter.matchFn(parsedUA) {
				return filter.clients, nil
			}
		}
	}

	msg := fmt.Sprintf("connected miner with id %s is unsupported", userAgent)
	return nil, errs.PoolError(errs.MinerUnknown, msg)
}
