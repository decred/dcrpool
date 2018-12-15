# dcrpool 
dcrpool is a decred mining pool. The nonce space is defined by haste protocol 
semantics which consistitutes 12-bytes of nonce space with an 8-bytes nonce 
and a 4-bytes worker id:  

`<0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00>, [0x00, 0x00, 0x00, 0x00]` 

It supports both PPS (Pay Per Share) and PPLNS (Pay Per Last N Shares) payment 
schemes. Clients require a registered account to the pool and must maintain a 
websockect connection for work notifications and submissions. The client 
included the project (poolclient) is a CPU mining client, therefore intended 
for testing purposes only. The pool through its connection to a node receives 
chain updates, particularly block notifications (connected & disconnected) as 
well as work notifications.  

Work notifications are relayed to connected pool clients with their respective 
pool targets which when solved are submitted to the pool. Pool shares are 
awarded for legit submissions. Legit work submissions from pool clients lower 
than the network target are submitted to the network.  

When a block is mined by the pool, the configured paymnt scheme (PPS or PPLNS) 
is applied to determine the proportions participating clients are due per their 
share count and share weights. The resulting amounts are recorded as payments 
and scheduled to paid to associated accounts after the block reward matures. 
The pool using its gRPC connection with to the pool wallet publishes payout 
transactions to associated pool accounts when block rewards mature. Processed 
payments are archived for auditing purposes. To install and run dcrpool:  

`git clone https://github.com/dnldd/dcrpool.git`  
`cd dcrpool`  
`go build`  
`go install`  
`dcrpool --configfile path/to/config.conf`

To install and run poolclient:  
`cd dcrpool/poolclient`  
`go build`  
`go install`  
`poolclient --configfile path/to/config.conf`


## mining harness  
The project has a tmux mining harness for testing purposes. The harness 
consists of two dcrd nodes, two wallets, a mining pool and a pool client. Refer 
to `harness.sh` for configuration details. The mining harness takes 10+ seconds 
to start so give it some time, or take a look at the script and better it :). 
To run the mining harness:  

`harness.sh`  


