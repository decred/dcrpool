# dcrpool 

[![Build Status](https://travis-ci.com/dnldd/dcrpool.svg?branch=master)](https://travis-ci.com/dnldd/dcrpool)

dcrpool is a stratum decred mining pool. It currently supports:
* Innosilicon D9 (port: 5552, supported firmware: [D9_20180602_094459.swu](https://drive.google.com/open?id=1wofB_OUDkB2gxz_IS7wM8Br6ogKdYDmY))
* Antminer DR3 (port: 5553)
* Antminer DR5 (port: 5554) 

The pool can be configured to mine in solo pool mode or as a publicly available 
mining pool. It supports both PPS (Pay Per Share) and PPLNS 
(Pay Per Last N Shares) payment schemes when configured as a publicly 
available mining pool. When configured as a solo pool, mining rewards 
accumulate at the specified mining address for the consensus daemon (dcrd).

To install and run dcrpool:  

```sh
git clone https://github.com/dnldd/dcrpool.git 
cd dcrpool 
go build 
go install 
dcrpool --configfile=path/to/config.conf 
```

The project has a tmux mining harness and a cpu miner for testing purposes.
Refer to `harness.sh` for configuration details. 

To install and run the cpu miner:  

```sh
cd dcrpool/miner 
go build 
go install 
miner --configfile=path/to/config.conf 
```

To run the mining harness:  

```sh
./harness.sh 
```


