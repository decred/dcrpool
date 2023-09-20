# dcrpool

[![Build Status](https://github.com/decred/dcrpool/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrpool/actions)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/decred/dcrpool)](https://goreportcard.com/report/github.com/decred/dcrpool)

## Overview

dcrpool is a stratum Decred mining pool.

---

WARNING: This does not currently work since it only supports the mining
algorithm used by Decred prior to the activation of
[DCP0011](https://github.com/decred/dcps/blob/master/dcp-0011/dcp-0011.mediawiki).

However, it is in the process of being updated to support the new algorithm.
This text will be updated once support has landed.

---

The default port all supported miners connect to the pool via is `:5550`.  The
pool can be configured to mine in solo pool mode or as a publicly available
mining pool.  Solo pool mode represents a private mining pool operation where
all connected miners to the pool are owned by the pool administrator.  For this
reason, mining rewards are left to accumulate at the specified address for the
mining node. There isn't a need for payment processing in solo pool mining mode,
it is disabled as a result.

In solo pool mining mode, miners only need to identify themselves when
connecting to the pool. The miner's username, specifically the username sent in
a `mining.authorize` message, should be a unique name identifying the client.

The pool supports Pay Per Share (`PPS`) and Pay Per Last N Shares (`PPLNS`)
payment schemes when configured for pool mining. With pool mining, mining
clients connect to the pool, contribute work towards solving a block and claim
shares for participation. When a block is found by the pool, portions of the
mining reward due participating accounts are calculated based on claimed shares
and the payment scheme used. The pool pays out the mining reward portions due
each participating account when it matures.

In addition to identifying itself to the pool, each connecting miner has to
specify the address its portion of the mining reward should be sent to when a
block is found. For this reason, the mining client's username is a combination
of the address mining rewards are paid to and its name, formatted as:
`address.name`. This username format for pool mining is required. The pool uses
the address provided in the username to create an account, all other connected
miners with the same address set will contribute work to that account.

The user interface of the pool provides public access to statistics and pool
account data. Users of the pool can access all payments, mined blocks by the
account and also work contributed by clients of the account via the interface.
The interface is only accessible via HTTPS and by default uses a self-signed
certificate, served on port `:8080`. In production, particularly for pool
mining, a certificate from an authority (`CA`) like
[letsencrypt](https://letsencrypt.org/) is recommended.

## Build and installation

- **Install Go 1.17 or higher**

  Installation instructions can be found here: https://golang.org/doc/install.
  Ensure Go was installed properly and is a supported version:
  ```sh
  $ go version
  $ go env GOROOT GOPATH
  ```
  NOTE: if `GOROOT` and `GOPATH` are initialized they must not be at the same path.
  It is recommended to add `$GOPATH/bin` to your `PATH` according to the Golang.org
  instructions.

- **Build and Install or Update dcrpool**

  The latest release of `dcrpool` may be built and installed with a single
  command without cloning this repository:

  ```sh
  $ go install github.com/decred/dcrpool@v1.2.0
  ```

  Using `@master` instead will perform a build using the latest code from the
  master branch.  This may be useful to use newer features or bug fixes not yet
  found in the latest release:

  ```sh
  $ go install github.com/decred/dcrpool@master
  ```

  Alternatively, a development build can be performed by running `go install` in
  a locally checked-out repository.

  In all cases, the `dcrpool` executable will be installed to the `bin`
  directory rooted at the path reported by `go env GOPATH`.

  Therefore, if you want to easily access `dcrpool` from the command-line
  without having to type the full path to the binary every time, ensure the
  aforementioned directory is added to your system path:

  * macOS: [how to add binary to your PATH](https://gist.github.com/nex3/c395b2f8fd4b02068be37c961301caa7#mac-os-x)
  * Windows: [how to add binary to your PATH](https://gist.github.com/nex3/c395b2f8fd4b02068be37c961301caa7#windows)
  * Linux and other Unix: [how to add binary to your PATH](https://gist.github.com/nex3/c395b2f8fd4b02068be37c961301caa7#linux)

## Database

dcrpool can run with either a [Bolt database](https://github.com/etcd-io/bbolt)
or a [Postgres database](https://www.postgresql.org/). Bolt is used by default.
[postgres.md](./docs/postgres.md) has more details about running with Postgres.

When running in Bolt mode, the pool maintains a backup of the database
(`backup.kv`), created on shutdown in the same directory as the database itself.
The user interface also provides functionality for pool administrators to backup
Bolt database when necessary.

## Configuration

dcrpool requires [dcrd](https://github.com/decred/dcrd) and
[dcrwallet](https://github.com/decred/dcrwallet) when configured as a mining
pool, it only requires dcrd when configured as a solo pool.
Deploying the user interface requires copying the `dcrpool/internal/gui/assets`
folder from source to a reachable location and updating the gui directory
(`--guidir`) of the configuration. Currently only single instance deployments
are supported, support for distributed deployments will be implemented in the
future.

### Example of a solo pool configuration

```no-highlight
rpcuser=user
rpcpass=pass
dcrdrpchost=127.0.0.1:9109
dcrdrpccert=/home/.dcrd/rpc.cert
solopool=true
activenet=mainnet
adminpass=adminpass
guidir=/home/gui
```

The configuration above uses a [Bolt database](https://github.com/etcd-io/bbolt). 
To switch to a [Postgres database](https://www.postgresql.org/) additional
config options will be needed, refer to [postgres.md](./docs/postgres.md).

### Example output of a solo pool startup

```no-highlight
dcrpool --configfile=pool.conf --appdata=/tmp/dcrpool-harness/pool
2020-12-22 20:10:31.120 [INF] POOL: Maximum work submission generation time at pool difficulty is 28s.
2020-12-22 20:10:31.129 [INF] POOL: Solo pool mode active.
2020-12-22 20:10:31.149 [INF] MP: Version: 1.1.0+dev
2020-12-22 20:10:31.149 [INF] MP: Runtime: Go version go1.15.6
2020-12-22 20:10:31.149 [INF] MP: Home dir: /tmp/dcrpool-harness/pool
2020-12-22 20:10:31.149 [INF] MP: Started dcrpool.
2020-12-22 20:10:31.149 [INF] GUI: Starting GUI server on port 8080 (https)
2020-12-22 20:10:31.149 [INF] MP: Creating profiling server listening on 127.0.0.1:6060
2020-12-22 20:10:31.150 [INF] POOL: listening on :5550
```

### Example of a mining pool configuration

```no-highlight
rpcuser=user
rpcpass=pass
dcrdrpchost=127.0.0.1:9109
dcrdrpccert=/home/.dcrd/rpc.cert
walletgrpchost=127.0.0.1:9111
walletrpccert=/home/.dcrwallet/rpc.cert
maxgentime=20s
solopool=false
activenet=mainnet
walletpass=walletpass
poolfeeaddrs=SsVPfV8yoMu7AvF5fGjxTGmQ57pGkaY6n8z
paymentmethod=pplns
lastnperiod=5m
adminpass=adminpass
guidir=/home/gui
```

The configuration above uses a [Bolt database](https://github.com/etcd-io/bbolt). 
To switch to a [Postgres database](https://www.postgresql.org/) additional
config options will be needed, refer to [postgres.md](./docs/postgres.md).

### Example output of a mining pool startup

```no-highlight
dcrpool --configfile=pool.conf --appdata=/tmp/dcrpool-harness/pool
2020-12-22 19:57:45.795 [INF] POOL: Maximum work submission generation time at pool difficulty is 20s.
2020-12-22 19:57:45.816 [INF] POOL: Payment method is PPLNS.
2020-12-22 19:57:45.916 [INF] MP: Version: 1.1.0+dev
2020-12-22 19:57:45.916 [INF] MP: Runtime: Go version go1.15.6
2020-12-22 19:57:45.916 [INF] MP: Creating profiling server listening on 127.0.0.1:6060
2020-12-22 19:57:45.916 [INF] MP: Home dir: /tmp/dcrpool-harness/pool
2020-12-22 19:57:45.917 [INF] MP: Started dcrpool.
2020-12-22 19:57:45.917 [INF] GUI: Starting GUI server on port 8080 (https)
2020-12-22 19:57:45.932 [INF] POOL: listening on :5550
```

Refer to [config descriptions](config.go) for more detail. 


## Wallet accounts

In mining pool mode the ideal wallet setup is to have two wallet accounts, the
pool account and the fee account, for the mining pool. This account structure
separates revenue earned from pool operations from mining rewards gotten on
behalf of participating clients. The pool account's purpose is to receive mining
rewards of the pool. The addresses generated from it should be the mining
addresses (`--miningaddr`) set for the mining node. It is important to set
multiple mining addresses for the mining node in production to make it difficult
for third-parties wanting to track coinbases mined by the pool and ultimately
determine the cumulative value of coinbases mined.

The fee account's purpose is to receive pool fees of the mining pool. It is
important to set multiple pool fee addresses to the mining pool in production to
make it difficult for third-parties wanting to track pool fees collected by the
pool and ultimately determine the cumulative value accrued by pool operators.
With multiple pool fee addresses set, the mining pool picks one at random for
every payout made. The addresses generated from the fee account should be set as
the pool fee addresses (`--poolfeeaddrs`) of the mining pool.

## Wallet Client Authentication

dcrwallet v1.6 requires client authentication certificates to be provided on
startup via the client CA file config option (`--clientcafile`).  Since dcrpool
is expected to maintain a grpc connection to the wallet it needs to generate the
needed certificate before the wallet is started. A config option
(`--gencertsonly`) which allows the generation of all key pairs without starting
the pool has been added for this purpose. Pool operators running a publicly
available mining pool will be required to first run their pools with
`--gencertsonly` to generate required key pairs before configuring their pool
wallets and starting them. The test harness, [harness.sh](./harness.sh),
provides a detailed example for reference. 

## Pool Fees

In mining pool mode pool fees collected by the pool operator are for maintaining
a connection to the decred network for the delivery of work to mining clients,
the submission of solved work by mining clients to the network and processing of
block rewards based on work contributed by participting accounts.

## Transaction fees

Every mature group of payments plus the pool fees collected completely exhaust
the referenced coinbases being sourced from by payments. For this reason payout
transactions by the pool create no change. It is implicit that the transaction
fees of the payout transaction are paid for by the accounts receiving payouts,
since transaction fees are not collected as part of pool fees. Each account
receiving a payout from the transaction pays a portion of the transaction fee
based on value of the payout in comparison to the overall value of payouts being
made by the transaction.

## Dust payments

Dust payments generated by participating accounts of the pool are forfeited to
the pool fees paid per each block mined. The reason for this is two-fold.  Dust
payments render payout transactions non-standard causing it to fail, also making
participating accounts forfeit dust payments serves as a good deterrent to
accounts that contribute intermittent, sporadic work. Participating accounts
become compelled to commit and contribute enough resources to the pool worth
more than dust outputs to guarantee receiving dividends whenever the pool mines
a block.

## Testing

The project has a configurable tmux mining harness and a CPU miner for testing
on simnet. Further documentation can be found in [harness.sh](./harness.sh).

## Should I be running dcrpool?

dcrpool is ideal for miners running medium-to-large mining operations. The
revenue generated from mining blocks as well as not paying pool fees to a
publicly available mining pool in the process should be enough to offset the
cost of running a pool. It will most likely not be cost effective to run dcrpool
for a small mining operation, the better option here would be using a public
mining pool instead.

For people looking to setup a publicly available mining pool, dcrpool's
well-documented configuration and simple setup process also make it a great
option.

## Contact

If you have any further questions you can find us at
https://decred.org/community/.

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrpool/issues)
is used for this project.

## License

dcrpool is licensed under the [copyfree](http://copyfree.org) ISC License.
