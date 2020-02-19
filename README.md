# Dcrpool 

[![Build Status](https://github.com/decred/dcrpool/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrpool/actions)


## Overview

Dcrpool is a stratum decred mining pool. It currently supports:

* Innosilicon D9 (default port: 5552, supported firmware: [D9_20180602_094459.swu](https://drive.google.com/open?id=1wofB_OUDkB2gxz_IS7wM8Br6ogKdYDmY))
* Antminer DR3 (default port: 5553)
* Antminer DR5 (default port: 5554)
* Whatsminer D1 (default port: 5555)

The pool can be configured to mine in solo pool mode or as a publicly available 
mining pool.  Solo pool mode represents a private mining pool operation where 
all connected miners to the pool are owned by the pool administrator.  For this 
reason, mining rewards are left to accumulate at the specified address for the 
mining node. There isn't a need for payment processing in solo pool mining mode, 
it is disabled as a result. 

In solo pool mining mode, miners only need to identify themselves when 
connecting to the pool. The miner's username, specifically the username sent 
in a `mining.authorize` message should be a unique name identifying the client.

The pool supports Pay Per Share (`PPS`) and Pay Per Last N Shares (`PPLNS`) 
payment schemes when configured for pool mining. With pool mining, mining 
clients connect to the pool, contribute work towards solving a block and 
claim shares for participation. When a block is found by the pool, portions of 
the mining reward due participating accounts are calculated based on claimed 
shares and the payment scheme used. The pool pays out the mining reward 
portions due each participating account when it matures.

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
[letsencrypt](https://letsencrypt.org/) is recommended. The user interface also 
provides pool administrators database backup functionality when needed.

## Installing and Updating

Building or updating from source requires the following build dependencies:

- **Go 1.12 or 1.13**

  Installation instructions can be found here: https://golang.org/doc/install.
  It is recommended to add `$GOPATH/bin` to your `PATH` at this point.

- **Git**

  Installation instructions can be found at https://git-scm.com or
  https://gitforwindows.org.

To build and install from a checked-out repo or a copy of the latest release, 
run `go install . ./cmd/...` in the root directory.  Some notes:

* Set the `GO111MODULE=on` environment variable.

* The `dcrpool` executable will be installed to `$GOPATH/bin`.  `GOPATH`
  defaults to `$HOME/go` (or `%USERPROFILE%\go` on Windows) if unset.


### Example of obtaining and building from source on Ubuntu:

```sh
git clone https://github.com/decred/dcrpool.git
cd dcrpool 
go install 
dcrpool --configfile=path/to/config.conf 
```

## Configuration

Dcrpool requires [dcrd](https://https://github.com/decred/dcrd) and [dcrwallet](https://https://github.com/decred/dcrwallet) when configured as a mining pool, it only requires dcrd when configured as a solo pool. 
Deploying the user interface requires copying the `dcrpool/gui/assets` folder from 
source to a reachable location and updating the gui directory (`--guidir`) of 
the configuration. Currently only single instance deployments are supported, 
support for distributed deployments will be implemented in the future.

### Example of a solo pool configuration:

```
rpcuser=user
rpcpass=pass
dcrdrpchost=127.0.0.1:19556
dcrdrpccert=/home/.dcrd/rpc.cert
solopool=true
activenet=mainnet
backuppass=backuppass
guidir=/home/gui
```

### Example of a mining pool configuration:

```
rpcuser=user
rpcpass=pass
dcrdrpchost=127.0.0.1:19556
dcrdrpccert=/home/.dcrd/rpc.cert
walletgrpchost=127.0.0.1:19558
walletrpccert=/home/.dcrwallet/rpc.cert
maxgentime=20s
solopool=false
activenet=simnet
walletpass=walletpass
poolfeeaddrs=SsVPfV8yoMu7AvF5fGjxTGmQ57pGkaY6n8z
paymentmethod=pplns
lastnperiod=5m
backuppass=backuppass
guidir=/home/gui
```

Refer to [config descriptions](config.go) for more detail.

## Wallet accounts

In mining pool mode the ideal wallet setup is to have two wallet accounts, 
the pool account and the fee account, for the mining pool. This account structure 
separates revenue earned from pool operations from mining rewards gotten on 
behalf of participating clients. The pool account's purpose is to receive 
mining rewards of the pool. The address generated from it should be the mining 
address (`--miningaddress`) of the mining node. The fee account's purpose is to 
receive pool fees of the mining pool. The address generated from it should be 
the address set as the pool fee address (`--poolfeeaddrs`) of the mining pool.

## Testing

The project has [a configurable tmux mining harness](harness.sh) and a cpu 
miner for testing. 

To run the mining harness:  

```sh
cd dcrpool/cmd/miner 
go install
cd ../..
./harness.sh 
```

## Should I be running dcrpool?

Dcrpool is ideal for miners running medium-to-large mining operations. The 
revenue generated from mining blocks as well as not paying pool fees to a 
publicly available mining pool in the process should be enough to offset 
the cost of running a pool. It will most likely not be cost effective to run dcrpool for a small mining operation, the better option here would be using  
a public mining pool instead. 

For people looking to setup a publicly available mining pool, dcrpool's well-documented configuration and simple setup process also make it
 a great option.

## Contact

If you have any further questions you can find us at https://decred.org/community

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrpool/issues)
is used for this project.

## License

dcrpool is licensed under the [copyfree](http://copyfree.org) ISC License.
