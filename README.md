# dcrpool 

[![Build Status](https://travis-ci.com/dnldd/dcrpool.svg?branch=master)](https://travis-ci.com/dnldd/dcrpool)

dcrpool is a stratum decred mining pool. It currently supports:
* Innosilicon D9 (port: 5552, supported firmware: [D9_20180602_094459.swu](https://drive.google.com/open?id=1wofB_OUDkB2gxz_IS7wM8Br6ogKdYDmY))
* Antminer DR3 (port: 5553)
* Antminer DR5 (port: 5554) 
* Whatsminer D1 (port: 5555)

The pool can be configured to mine in solo pool mode or as a publicly available 
mining pool. It supports both PPS (Pay Per Share) and PPLNS 
(Pay Per Last N Shares) payment schemes when configured as a publicly 
available mining pool. When configured as a solo pool, mining rewards 
accumulate at the specified mining address for the consensus daemon (dcrd).

To install and run dcrpool:  

```sh
git clone https://github.com/dnldd/dcrpool.git 
cd dcrpool 
go install 
dcrpool --configfile=path/to/config.conf 
```

The project has a tmux mining harness and a cpu miner coupled with the simnet 
network for testing.
Refer to `harness.sh` for configuration details. 

To install and run the cpu miner:  

```sh
cd dcrpool/cmd/miner 
go install 
miner --configfile=path/to/config.conf 
```

To run the mining harness:  

```sh
cd dcrpool
./harness.sh 
```

dcpool provides API access to mining pool data on. It currently has the following calls available:
```
GET /hash - maximum estimated hash of connected pool clients.

GET /connections - number of connected pool clients.

GET /work/quotes [pooled mining call] - PPS/PPLNS work quotas for participating pool clients. 

GET /work/height - the recent work height.

GET /payment/height - the last payment height.

POST /account/mined - list of mined blocks by account.
payload: {
	"name":"xxx", - the account name.
	"address": "xxx" - the account address.
}

GET /mined/{page} - paginated list of mined blocks by the pool.

POST /account/payments [pooled mining call] - list of payments made to the provided account.
payload: {
	"name":"xxx", - the account name.
	"address": "xxx", - the account address.
	"min": xxxx - the minimum payment time, in seconds, unix time.
}
```

Thanks to davecgh, SweeperAA, dhill, jhartbarger and NickH for their contributions.
