## Breaking Changes.
dcrpool v1.1.0 introduces breaking changes to the configuration, existing configurations will have to be updated accordingly. The breaking 
changes made are:
 1. `--backuppass` has been renamed to `--adminpass`.

 1. `--lastnperiod` and `--maxgentime` are now of type `time.Duration`.

## Bug Fixes.
 The notable bug fixes in this release include:
   1. Data access issues related to using raw db values outside of their associated database transactions have been resolved.

   1. Race conditions associated with listing websocket clients and updating the GUI have been resolved.

   1. Panics associated with the cpu miner in the testing harness have been resolved.

   1. A case where a client stalling on work does not receive timestamp-rolled work has been resolved.

   1. Panics associated with logging when the logger has not been initialized have been resolved.

   1. A PPS bug where shares generated outside the range of the confirmed work are used in generating payments has been fixed.

## Improvements.
The notable infrastructure and interface improvements include:
 1. UI/UX has been refreshed with the aim of simplifying usage patterns.

 1. Reworked payments processing by having payments track the coinbases being sourced from as well as ensuring payouts are made immediately when the associated coinbase is mature and spendable. For this reason, payout transactions do not return change.

 1. Test coverage for pool components have been improved to ensure the 
 components do what they are expected to under various conditions and also 
 ensure added features do not break expected behaviour.

 1. Support for the Obelisk DCR1 has been added. dcrpool currently supports all publicly available ASICs for decred at the moment.

 1. `--maxconnectionsperhost` configuration option can now be used to limit the number of connections the pool will accept from a single host, the default limit is a 100 clients. 

 1. Work is now delivered via dcrd's notifywork which sends work notifications which is more efficient than polling the `getwork` rpc.
 
 1. Timestamp-rolled work support has been added to distribute updated work 
 miners stall on work submissions.

 1. Added `--walletaccount` config option.

 1. GUI caching and cache update signalling have been added to reduce round-trips to the database for requested data and efficiently refresh the the data cached.

 1. Improved test harness by adding configurable number of mining clients.

 1. Mined work, reward quota, pending and archived payment requests by the GUI are now paginated. 

 1. The pool now utiilizes transaction confirmation notification streams to ensure coinbases are spendable before the pool utilizes them in payout transaction.

 1. The allowed pool fee range has been updated to between 0.2% and 5%, bounds inclusive. 

1. Deprecated `--minpayment` & `--maxtxfeereserve` in favour of an improved payment process which sources tx fees from the receiving account and does not generate change.

