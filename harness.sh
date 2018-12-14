# Tmux script that sets up a mining harness. 
set -e
SESSION="harness"
NODES_ROOT=~/harness
RPCUSER="user"
RPCPASS="pass"
MINING_USER="pcl"
MINING_PASS="pass"
WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
TBY_WALLET_SEED="1111111111111111111111111111111111111111111111111111111111111111"
POOL_MINING_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
PFEE_ADDR="SsVPfV8yoMu7AvF5fGjxTGmQ57pGkaY6n8z" 
CLIENT_ADDR="SsZckVrqHRBtvhJA5UqLZ3MDXpZHi5mK6uU"
MINING_ADDR="Ssmn12w4CTF2j6B1jaLxEyKXVeMFkPJmDBs"

if [ -d "${NODES_ROOT}" ] ; then
  rm -R "${NODES_ROOT}"
fi

case "$OSTYPE" in
  solaris*) APPDATA="${HOME}" ;;
  darwin*)  APPDATA="${HOME}/Library/Application Support" ;; 
  linux*)   APPDATA="${HOME}" ;;
  bsd*)     APPDATA="${HOME}" ;;
esac

DCRD_RPC_CERT="${APPDATA}/Dcrd/rpc.cert"
DCRW_RPC_CERT="${APPDATA}/Dcrwallet/rpc.cert"
DCRW_RPC_KEY="${APPDATA}/Dcrwallet/rpc.key"

mkdir -p "${NODES_ROOT}/"{master,cpuminer,wallet,tbywallet,pool,client}
cat > "${NODES_ROOT}/master/dcrd.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
rpccert=${DCRD_RPC_CERT}
simnet=1
logdir=./log
datadir=./data
debuglevel=TXMP=debug,MINR=debug,BMGR=debug,SRVR=debug
miningaddr=${POOL_MINING_ADDR}
EOF

cat > "${NODES_ROOT}/cpuminer/dcrd.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
rpccert=${DCRD_RPC_CERT}
simnet=1
logdir=./log
datadir=./data
rpclisten=127.0.0.1:18556
connect=127.0.0.1:18555
debuglevel=TXMP=debug,MINR=debug,BMGR=debug,SRVR=debug
miningaddr=${MINING_ADDR}
EOF

cat > "${NODES_ROOT}/dcrctl.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
simnet=1
EOF

cat > "${NODES_ROOT}/wallet/wallet.conf" <<EOF
username=${RPCUSER}
password=${RPCPASS}
simnet=1
rpccert=${DCRW_RPC_CERT}
rpckey=${DCRW_RPC_KEY}
logdir=./log
appdata=./data
pass=123
debuglevel=debug
EOF

cat > "${NODES_ROOT}/tbywallet/tbywallet.conf" <<EOF
username=${RPCUSER}
password=${RPCPASS}
simnet=1
rpccert=${DCRW_RPC_CERT}
rpckey=${DCRW_RPC_KEY}
logdir=./log
appdata=./data
pass=123
enablevoting=1
enableticketbuyer=1
rpclisten=127.0.0.1:19559
nogrpc=1
ticketbuyer.nospreadticketpurchases=1
ticketbuyer.maxperblock=5
ticketbuyer.balancetomaintainrelative=0
ticketbuyer.maxpriceabsolute=0.2
debuglevel=debug
EOF

cat > "${NODES_ROOT}/pool/pool.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
dcrdrpchost=127.0.0.1:19556
walletgrpchost=127.0.0.1:19558
debuglevel=trace
maxgentime=5
port=:19560
walletpass=123
activenet=simnet
poolfeeaddrs=${PFEE_ADDR}
paymentmethod=pplns
lastnperiod=10
; paymentmethod=pps
EOF

cat > "${NODES_ROOT}/client/client.conf" <<EOF
debuglevel=trace
user=${MINING_USER}
pass=${MINING_PASS}
host=127.0.0.1:19560
minertype=cpu
EOF

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION

################################################################################
# Setup the master node.
################################################################################
cat > "${NODES_ROOT}/master/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/master/ctl"

tmux rename-window -t $SESSION:1 'master'
tmux send-keys "cd ${NODES_ROOT}/master" C-m
tmux send-keys "dcrd -C dcrd.conf" C-m

################################################################################
# Setup the master node's dcrctl (mctl).
################################################################################
tmux new-window -t $SESSION:2 -n 'mctl'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

################################################################################
# Setup the cpu mining node.
################################################################################
cat > "${NODES_ROOT}/cpuminer/mine" <<EOF
#!/bin/zsh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ../dcrctl.conf --rpcserver 127.0.0.1:18556 generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/cpuminer/mine"

cat > "${NODES_ROOT}/cpuminer/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf --rpcserver 127.0.0.1:18556 \$*
EOF
chmod +x "${NODES_ROOT}/cpuminer/ctl"

tmux new-window -t $SESSION:3 -n 'cpuminer'
tmux send-keys "cd ${NODES_ROOT}/cpuminer" C-m
tmux send-keys "dcrd -C dcrd.conf" C-m

################################################################################
# Setup the pool wallet.
################################################################################
cat > "${NODES_ROOT}/wallet/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/wallet/ctl"

tmux new-window -t $SESSION:4 -n 'wallet'
tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
tmux send-keys "dcrwallet -C wallet.conf --create" C-m
sleep 2
tmux send-keys "123" C-m "123" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C wallet.conf" C-m

################################################################################
# Setup the ticket buying wallet.
################################################################################
cat > "${NODES_ROOT}/tbywallet/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf --wallet --walletrpcserver 127.0.0.1:19559 \$*
EOF
chmod +x "${NODES_ROOT}/tbywallet/ctl"

tmux new-window -t $SESSION:5 -n 'tbywallet'
tmux send-keys "cd ${NODES_ROOT}/tbywallet" C-m
tmux send-keys "dcrwallet -C tbywallet.conf --create" C-m
sleep 2
tmux send-keys "123" C-m "123" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${TBY_WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C tbywallet.conf" C-m

################################################################################
# Setup the pool wallet's dcrctl (wctl).
################################################################################
sleep 20
tmux new-window -t $SESSION:6 -n 'wctl'
tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
tmux send-keys "./ctl createnewaccount pfee" C-m
sleep 1
tmux send-keys "./ctl getnewaddress pfee" C-m
sleep 1
tmux send-keys "./ctl createnewaccount client" C-m
sleep 1
tmux send-keys "./ctl getnewaddress client" C-m
sleep 1
tmux send-keys "./ctl getnewaddress default" C-m
sleep 1
tmux send-keys "./ctl getbalance"

################################################################################
# Setup ticket buying wallet's dcrctl (tctl).
################################################################################
tmux new-window -t $SESSION:7 -n 'tctl'
tmux send-keys "cd ${NODES_ROOT}/tbywallet" C-m
tmux send-keys "./ctl getnewaddress" C-m
sleep 1
tmux send-keys "./ctl getbalance"

################################################################################
# Setup the cpu miner's dcrctl (cctl).
################################################################################
sleep 1
tmux new-window -t $SESSION:8 -n 'cctl'
tmux send-keys "cd ${NODES_ROOT}/cpuminer" C-m

# mine the first 5 blocks, this circumvents a wallet bug which occurs when the 
# wallet is connected before block 1 is mined. This also provides enough funds 
# for the ticket buyer.
tmux send-keys "./mine 5" C-m

################################################################################
# Setup dcrpool.
################################################################################
tmux new-window -t $SESSION:9 -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile pool.conf --homedir=." C-m
sleep 2
tmux send-keys `curl -s POST http://127.0.0.1:19560/create/account \
  -H 'Cache-Control: no-cache' \
  -H 'Content-Type: application/json' \
  -d '{"name": "pcl", "address": "SsZckVrqHRBtvhJA5UqLZ3MDXpZHi5mK6uU" ,"pass": "pass"}' \
  > /dev/null`

################################################################################
# Setup the mining client. 
################################################################################
sleep 2
tmux new-window -t $SESSION:10 -n 'client'
tmux send-keys "cd ${NODES_ROOT}/client" C-m
tmux send-keys "poolclient --configfile client.conf --homedir=." C-m

tmux attach-session -t $SESSION