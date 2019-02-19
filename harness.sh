# Tmux script that sets up a mining harness. 
set -e
SESSION="harness"
NODES_ROOT=~/harness
RPC_USER="user"
RPC_PASS="pass"
WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
NETWORK="testnet3"

if [ "${NETWORK}" = "simnet" ]; then
 POOL_MINING_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
 PFEE_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
 CLIENT_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
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


if [ -d "${NODES_ROOT}" ] ; then
  rm -R "${NODES_ROOT}"
fi

DCRD_RPC_CERT="${HOME}/.dcrd/rpc.cert"
DCRD_RPC_KEY="${HOME}/.dcrd/rpc.key"
WALLET_RPC_CERT="${HOME}/.dcrwallet/rpc.cert"
WALLET_RPC_KEY="${HOME}/.dcrwallet/rpc.key"

mkdir -p "${NODES_ROOT}/master"
mkdir -p "${NODES_ROOT}/wallet"
mkdir -p "${NODES_ROOT}/pool"
mkdir -p "${NODES_ROOT}/client"

cat > "${NODES_ROOT}/wallet/wallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
rpccert=${WALLET_RPC_CERT}
rpckey=${WALLET_RPC_KEY}
logdir=./log
appdata=./data
pass=123
debuglevel=debug
EOF

cat > "${NODES_ROOT}/client/client.conf" <<EOF
debuglevel=trace
activenet=${NETWORK}
user=miner
address=${CLIENT_ADDR}
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
homedir=.
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:9109
dcrdrpccert=${DCRD_RPC_CERT}
debuglevel=trace
maxgentime=5
solopool=1
activenet=${NETWORK}
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
homedir=.
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:19556
dcrdrpccert=${DCRD_RPC_CERT}
debuglevel=trace
maxgentime=5
solopool=1
activenet=${NETWORK}
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
homedir=.
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:19109
dcrdrpccert=${DCRD_RPC_CERT}
debuglevel=trace
maxgentime=5
solopool=1
activenet=${NETWORK}
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

tmux rename-window -t $SESSION:1 'master'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

if [ "${NETWORK}" = "mainnet" ]; then
tmux send-keys "dcrd -C ${HOME}/dcrd.conf \
--rpccert=${DCRD_RPC_CERT} --rpckey=${DCRD_RPC_KEY} \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR}" C-m
fi

if [ "${NETWORK}" = "testnet3" ]; then
tmux send-keys "dcrd -C ${HOME}/dcrd.conf \
--rpccert=${DCRD_RPC_CERT} --rpckey=${DCRD_RPC_KEY} \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR} \
--testnet" C-m
fi

if [ "${NETWORK}" = "simnet" ]; then
tmux send-keys "dcrd -C ${HOME}/dcrd.conf --appdata=${NODES_ROOT}/master/data \
--rpccert=${DCRD_RPC_CERT} --rpckey=${DCRD_RPC_KEY} \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR} \
--simnet" C-m
fi

################################################################################
# Setup the master node's dcrctl (mctl).
################################################################################
sleep 3
cat > "${NODES_ROOT}/master/mine" <<EOF
#!/bin/zsh
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

sleep 2
tmux new-window -t $SESSION:2 -n 'mctl'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

# mine some blocks to start the chain.
# mux send-keys "./mine 2" C-m

################################################################################
# Setup the pool wallet.
################################################################################
# cat > "${NODES_ROOT}/wallet/ctl" <<EOF
# #!/bin/zsh
# dcrctl -C ../dcrctl.conf --wallet \$*
# EOF
# chmod +x "${NODES_ROOT}/wallet/ctl"

# tmux new-window -t $SESSION:3 -n 'wallet'
# tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
# tmux send-keys "dcrwallet -C wallet.conf --create" C-m
# sleep 2
# tmux send-keys "123" C-m "123" C-m "n" C-m "y" C-m
# sleep 1
# tmux send-keys "${WALLET_SEED}" C-m C-m
# tmux send-keys "dcrwallet -C wallet.conf" C-m

################################################################################
# Setup the pool wallet's dcrctl (wctl).
################################################################################
# sleep 3
# tmux new-window -t $SESSION:4 -n 'wctl'
# tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
# tmux send-keys "./ctl createnewaccount pfee" C-m
# tmux send-keys "./ctl getnewaddress pfee" C-m
# tmux send-keys "./ctl createnewaccount client" C-m
# tmux send-keys "./ctl getnewaddress client" C-m
# tmux send-keys "./ctl getnewaddress default" C-m
# tmux send-keys "./ctl getbalance"

################################################################################
# Setup dcrpool.
################################################################################
sleep 4
tmux new-window -t $SESSION:5 -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile pool.conf" C-m

################################################################################
# Setup the mining client. 
################################################################################
# sleep 1
# tmux new-window -t $SESSION:6 -n 'client'
# tmux send-keys "cd ${NODES_ROOT}/client" C-m
# tmux send-keys "miner --configfile=client.conf --homedir=." C-m

tmux attach-session -t $SESSION