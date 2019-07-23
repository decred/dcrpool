# dcrpool 

[![Build Status](https://travis-ci.com/decred/dcrpool.svg?branch=master)](https://travis-ci.com/decred/dcrpool)

dcrpool is a stratum decred mining pool. It currently supports:

* Innosilicon D9 (default port: 5552, supported firmware: [D9_20180602_094459.swu](https://drive.google.com/open?id=1wofB_OUDkB2gxz_IS7wM8Br6ogKdYDmY))
* Antminer DR3 (default port: 5553)
* Antminer DR5 (default port: 5554)
* Whatsminer D1 (default port: 5555)

The pool can be configured to mine in solo pool mode or as a publicly available  
mining pool.  Solo pool mode represents a private mining pool operation where  
all connected miners to the pool are owned by the pool administrator.  
For this reason, mining rewards are left to accumulate at the specified  
address for the mining node. There isn't a need for payment processing in solo  
pool mining mode, it is disabled as a result. 

In solo pool mining mode, miners only need to identify themselves when  
connecting to the pool. The miner's username, specifically the username  
sent in a `mining.authorize` message should be a unique name identifying  
the client.

Here is a sample config for solo mining:

```
rpcuser=RPC_USER
rpcpass=RPC_PASS
dcrdrpchost=DCRD_RPC_HOST
dcrdrpccert=DCRD_RPC_CERT
debuglevel=trace
solopool=true
activenet=NETWORK
backuppass=BACKUP_PASS
guidir=GUI_DIR
```

Refer to config descriptions for more detail on the options.

The pool supports Pay Per Share (`PPS`) and Pay Per Last N Shares (`PPLNS`)  
payment schemes when configured for pool mining.

With pool mining, mining clients connect to the pool, contribute work towards  
solving a block and claim shares for participation. When a block is found by  
the pool, portions of the mining reward due participating accounts are  
calculated based on claimed shares and the payment scheme used. The pool pays  
out the mining reward portions due each participating account when it matures.

In addition to identifying itself to the pool, each connecting miner has to  
specify the address its portion of the mining reward should be sent to   
when a block is found. For this reason, the mining client's username is a  
combination of the address mining rewards are paid to and its name,  
formatted as: `address.name`. This username format for pool mining is  
required. The pool uses the address provided in the username to create  
an account, all other connected miners with the same address set will   
contribute work to that account.  

Here is a sample config for pool mining:
```
rpcuser=RPC_USER
rpcpass=RPC_PASS
dcrdrpchost=DCRD_RPC_HOST
dcrdrpccert=DCRD_RPC_CERT
walletgrpchost=WALLER_GRPC_HOST
walletrpccert=WALLET_RPC_CERT
debuglevel=trace
maxgentime=MAX_GEN_TIME
solopool=false
activenet=NETWORK
walletpass=WALLET_PASS
poolfeeaddrs=PFEE_ADDR
paymentmethod=PAYMENT_METHOD
lastnperiod=LAST_N_PERIOD
backuppass=BACKUP_PASS
guidir=GUI_DIR
```

Refer to config descriptions for more detail on the options.

The user interface of the pool provides public access to statistics and pool  
account data. Users of the pool can access all payments, mined blocks by the  
account and also work contributed by clients of the account via the interface.  
The interface is only accessible via HTTPS and by default uses a self-signed  
certificate, served on port `:8080`. In production, particularly for pool  
mining, a certificate from an authority (`CA`) like [letsencrypt](https://letsencrypt.org/) is recommended.  
The user interface also provides pool administrators database backup  
functionality when needed.

To deploy the user interface, copy `dcrpool/gui/assets` folder to a reachable 
location and update the gui directory (`--guidir`) of the 
configuration.

To install and run dcrpool:  

```sh
git clone https://github.com/decred/dcrpool.git
cd dcrpool 
go install 
dcrpool --configfile=path/to/config.conf 
```

The project has a tmux mining harness and a cpu miner for testing.  
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

Thanks to davecgh, SweeperAA, dhill, jhartbarger, NickH and jholdstock for their
contributions.
