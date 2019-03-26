#!/bin/sh
# Tmux script that sets up a mining harness. 
set -e
SESSION="harness"
NODES_ROOT=~/harness
RPC_USER="user"
RPC_PASS="pass"
WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
WALLET_PASS=123
NETWORK="simnet"
# NETWORK="testnet3"
# NETWORK="mainnet"
SOLO_POOL=0
MAX_GEN_TIME=20
PAYMENT_METHOD="pplns"
LAST_N_PERIOD=300 # PPLNS range, 5 minutes.

if [ "${NETWORK}" = "simnet" ]; then
 POOL_MINING_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
 PFEE_ADDR="SsVPfV8yoMu7AvF5fGjxTGmQ57pGkaY6n8z"
 CLIENT_ONE_ADDR="SsZckVrqHRBtvhJA5UqLZ3MDXpZHi5mK6uU"
 CLIENT_TWO_ADDR="Ssn23a3rJaCUxjqXiVSNwU6FxV45sLkiFpz"
fi

if [ "${NETWORK}" = "mainnet" ]; then
POOL_MINING_ADDR="DsXete8zpkHZXGd4vcxhz4rPqmEqydC9u5E"
PFEE_ADDR="DsXete8zpkHZXGd4vcxhz4rPqmEqydC9u5E"
CLIENT_ADDR="DsXete8zpkHZXGd4vcxhz4rPqmEqydC9u5E"
fi

if [ "${NETWORK}" = "testnet3" ]; then
POOL_MINING_ADDR="TsWP4MhHZn77F6tQprHhFoJHMWifaDdh2Mc"
PFEE_ADDR="TsWP4MhHZn77F6tQprHhFoJHMWifaDdh2Mc"
CLIENT_ADDR="TsWP4MhHZn77F6tQprHhFoJHMWifaDdh2Mc"
fi


if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

DCRD_RPC_CERT="${HOME}/.dcrd/rpc.cert"
DCRD_RPC_KEY="${HOME}/.dcrd/rpc.key"
DCRD_CONF="${HOME}/.dcrd/dcrd.conf"
WALLET_RPC_CERT="${HOME}/.dcrwallet/rpc.cert"
WALLET_RPC_KEY="${HOME}/.dcrwallet/rpc.key"

mkdir -p "${NODES_ROOT}/master"
mkdir -p "${NODES_ROOT}/wallet"
mkdir -p "${NODES_ROOT}/pool"
mkdir -p "${NODES_ROOT}/c1"
mkdir -p "${NODES_ROOT}/c2"

cat > "${NODES_ROOT}/c1/client.conf" <<EOF
debuglevel=trace
activenet=${NETWORK}
user=m1
address=${CLIENT_ONE_ADDR}
pool=127.0.0.1:5550
EOF

cat > "${NODES_ROOT}/c2/client.conf" <<EOF
debuglevel=trace
activenet=${NETWORK}
user=m2
address=${CLIENT_TWO_ADDR}
pool=127.0.0.1:5550
EOF

if [ "${NETWORK}" = "mainnet" ]; then
cat > "${NODES_ROOT}/dcrctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${DCRD_RPC_CERT}
rpcserver=127.0.0.1:9109
EOF

cat > "${NODES_ROOT}/pool/pool.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:9109
dcrdrpccert=${DCRD_RPC_CERT}
walletgrpchost=127.0.0.1:9111
walletrpccert=${WALLET_RPC_CERT}
debuglevel=trace
maxgentime=${MAX_GEN_TIME}
solopool=${SOLO_POOL}
activenet=${NETWORK}
walletpass=${WALLET_PASS}
poolfeeaddrs=${PFEE_ADDR}
paymentmethod=${PAYMENT_METHOD}
lastnperiod=${LAST_N_PERIOD}
EOF
fi

if [ "${NETWORK}" = "simnet" ]; then
cat > "${NODES_ROOT}/dcrctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${DCRD_RPC_CERT}
rpcserver=127.0.0.1:19556
EOF

cat > "${NODES_ROOT}/pool/pool.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:19556
dcrdrpccert=${DCRD_RPC_CERT}
walletgrpchost=127.0.0.1:19558
walletrpccert=${WALLET_RPC_CERT}
debuglevel=trace
maxgentime=${MAX_GEN_TIME}
solopool=${SOLO_POOL}
activenet=${NETWORK}
walletpass=${WALLET_PASS}
poolfeeaddrs=${PFEE_ADDR}
paymentmethod=${PAYMENT_METHOD}
lastnperiod=${LAST_N_PERIOD}
EOF

cat > "${NODES_ROOT}/dcrwctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${WALLET_RPC_CERT}
rpcserver=127.0.0.1:19557
EOF

cat > "${NODES_ROOT}/wallet/wallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
rpccert=${WALLET_RPC_CERT}
rpckey=${WALLET_RPC_KEY}
logdir=./log
appdata=./data
simnet=1
pass=${WALLET_PASS}
debuglevel=debug
EOF
fi

if [ "${NETWORK}" = "testnet3" ]; then
cat > "${NODES_ROOT}/dcrctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${DCRD_RPC_CERT}
rpcserver=127.0.0.1:19109
EOF

cat > "${NODES_ROOT}/pool/pool.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:19109
dcrdrpccert=${DCRD_RPC_CERT}
walletgrpchost=127.0.0.1:19111
walletrpccert=${WALLET_RPC_CERT}
debuglevel=trace
maxgentime=${MAX_GEN_TIME}
solopool=${SOLO_POOL}
activenet=${NETWORK}
walletpass=${WALLET_PASS}
poolfeeaddrs=${PFEE_ADDR}
paymentmethod=${PAYMENT_METHOD}
lastnperiod=${LAST_N_PERIOD}
EOF
fi

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION

################################################################################
# Setup the master node.
################################################################################
cat > "${NODES_ROOT}/master/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/master/ctl"

tmux rename-window -t $SESSION:0 'master'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

if [ "${NETWORK}" = "mainnet" ]; then
tmux send-keys "dcrd -C ${DCRD_CONF} \
--rpccert=${DCRD_RPC_CERT} --rpckey=${DCRD_RPC_KEY} \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR}" C-m
fi

if [ "${NETWORK}" = "testnet3" ]; then
tmux send-keys "dcrd -C ${DCRD_CONF} \
--rpccert=${DCRD_RPC_CERT} --rpckey=${DCRD_RPC_KEY} \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR} \
--testnet" C-m
fi

if [ "${NETWORK}" = "simnet" ]; then
tmux send-keys "dcrd -C ${DCRD_CONF} \
--rpccert=${DCRD_RPC_CERT} --rpckey=${DCRD_RPC_KEY} \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR} \
--simnet" C-m
fi

################################################################################
# Setup the master node's dcrctl (mctl).
################################################################################
cat > "${NODES_ROOT}/master/mine" <<EOF
#!/bin/sh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ../dcrctl.conf  generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/master/mine"

tmux new-window -t $SESSION:1 -n 'mctl'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

if [ "${NETWORK}" = "simnet" ]; then
sleep 2
# mine some blocks to start the chain if the active network is simnet.
tmux send-keys "./mine 2" C-m
fi

if [ "${NETWORK}" = "simnet" ]; then
################################################################################
# Setup the pool wallet.
################################################################################
cat > "${NODES_ROOT}/wallet/ctl" <<EOF
#!/bin/sh
dcrctl -C ../dcrwctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/wallet/ctl"

tmux new-window -t $SESSION:2 -n 'wallet'
tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
tmux send-keys "dcrwallet -C wallet.conf --create" C-m
sleep 2
tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C wallet.conf" C-m

################################################################################
# Setup the pool wallet's dcrctl (wctl).
################################################################################
sleep 6
# The consensus daemon must be synced for account generation to 
# work as expected.
tmux new-window -t $SESSION:3 -n 'wctl'
tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
tmux send-keys "./ctl createnewaccount pfee" C-m
tmux send-keys "./ctl getnewaddress pfee" C-m
tmux send-keys "./ctl createnewaccount c1" C-m
tmux send-keys "./ctl getnewaddress c1" C-m
tmux send-keys "./ctl createnewaccount c2" C-m
tmux send-keys "./ctl getnewaddress c2" C-m
tmux send-keys "./ctl getnewaddress default" C-m
tmux send-keys "./ctl getbalance"
fi

################################################################################
# Setup dcrpool.
################################################################################
sleep 4
tmux new-window -t $SESSION:4 -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile pool.conf --homedir=${NODES_ROOT}/pool" C-m

if [ "${NETWORK}" = "simnet" ]; then
################################################################################
# Setup first mining client. 
################################################################################
sleep 1
tmux new-window -t $SESSION:5 -n 'c1'
tmux send-keys "cd ${NODES_ROOT}/c1" C-m
tmux send-keys "miner --configfile=client.conf --homedir=${NODES_ROOT}/c1" C-m

################################################################################
# Setup another mining client. 
################################################################################
sleep 1
tmux new-window -t $SESSION:6 -n 'c2'
tmux send-keys "cd ${NODES_ROOT}/c2" C-m
tmux send-keys "miner --configfile=client.conf --homedir=${NODES_ROOT}/c2" C-m
fi

tmux attach-session -t $SESSION