# dcrpool v2.0.0

This is a new major version of dcrpool which has been updated to use the new BLAKE3 mining algorithm and removes support for ASIC miners.

*Note:* This version of dcrpool is not compatible with databases created by older versions of dcrpool.

## Breaking Changes

1. `--service` config flag has been removed. ([#342](https://github.com/decred/dcrpool/pull/342))

1. Database version reset to 1 for dcrpool 2.0.0. ([#391](https://github.com/decred/dcrpool/pull/391))

1. Deprecated `--minpayment` and `--maxtxfeereserve` config flags have been removed. ([#393](https://github.com/decred/dcrpool/pull/393))

1. Support for ASIC miners removed (Whatsminer D1, Antminer DR5, Antminer DR3 Innosilicon D9, Obelisk DCR1). ([#400](https://github.com/decred/dcrpool/pull/400))

1. BLAKE256 support removed in favour of BLAKE3. ([#412](https://github.com/decred/dcrpool/pull/412))

## Improvements

1. CPU miner updated to support BLAKE3 mining. ([#341](https://github.com/decred/dcrpool/pull/341))

1. Git hash is now included in application version where possible. ([#355](https://github.com/decred/dcrpool/pull/355))

1. Minimum TLS version for dcrwallet connection is now 1.2. ([#369](https://github.com/decred/dcrpool/pull/369))

1. Improved shutdown signal handling for Windows and Unix platforms. ([#381](https://github.com/decred/dcrpool/pull/381))

1. Postgres 14.9, 15.4, and 16.0 tested and confirmed to be compatible. ([#384](https://github.com/decred/dcrpool/pull/384))

1. Payment IDs are now generated with some randomness to prevent collisions. ([#392](https://github.com/decred/dcrpool/pull/392))

1. Client versions are now only validated by major and minor version numbers, not patch number. ([#406](https://github.com/decred/dcrpool/pull/406))

1. gominer 2.0.x added as a supported client. ([#413](https://github.com/decred/dcrpool/pull/413))

1. Client IP and port are now displayed in the account page. ([#428](https://github.com/decred/dcrpool/pull/428))

## Bug Fixes

1. Work performed on blocks which were reorged out of the best chain is now properly pruned. ([#336](https://github.com/decred/dcrpool/pull/336))

1. Tasks deferred until shutdown are now always executed if an error is encountered during startup. ([#351](https://github.com/decred/dcrpool/pull/351))

1. Database is properly closed when dcrpool shuts down due to an error. ([#353](https://github.com/decred/dcrpool/pull/353))

1. Subscribe requests which fail to fetch miner difficulty will now receive a proper error response instead of nothing. ([#372](https://github.com/decred/dcrpool/pull/372))

1. Attempting to message a client on a closed connection will no longer cause the process to hang. ([#375](https://github.com/decred/dcrpool/pull/375))

1. Fixed a race in payment manager logic. ([#396](https://github.com/decred/dcrpool/pull/396))

1. Prevent a possible panic during shutdown. ([#397](https://github.com/decred/dcrpool/pull/397))

1. Fixed a race in client connection handler. ([#402](https://github.com/decred/dcrpool/pull/402))

1. Reward payments are no longer paying excessively large fees. ([#427](https://github.com/decred/dcrpool/pull/427))

1. Fix incorrect handling when dcrd is not able to find a transaction. ([#431](https://github.com/decred/dcrpool/pull/431))

## Code Contributors (alphabetical order)

- Dave Collins
- David Hill
- Donald Adu-Poku
- Jamie Holdstock
