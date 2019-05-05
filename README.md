# dcrpool 

[![Build Status](https://travis-ci.com/decred/dcrpool.svg?branch=master)](https://travis-ci.com/decred/dcrpool)

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

dcpool provides web ui which is available on port 8080 by default.

To install and run dcrpool:  

```sh
git clone https://github.com/decred/dcrpool.git
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

Thanks to davecgh, SweeperAA, dhill, jhartbarger and NickH for their contributions.
