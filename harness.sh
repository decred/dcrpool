#!/bin/sh
# Tmux script that creates a cpu mining test harness with 2 dcrd nodes, 
# a dcrpool instance and a dcrpclient instance.
# Network layout:
# master [listen:19555]  <->  pnode [listen:19565, rpc:19566] <-> dcrctl
#                                  \
#                                   <-> pool [ws:19576]
#                                            \
#                                             <-> client 
set -e
SESSION="harness"
NODES_ROOT=~/harness
RPCUSER="user"
RPCPASS="pass"
MINING_USER="pcl"
MINING_PASS="pass"
WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
WALLET_MINING_ADDR="SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc" # NOTE: This must be changed if the seed is changed.
WALLET_XFER_ADDR="Sso52TPnorVkSaRYzHmi4FgU8F5BFEDZsiK" # same as above
APPDATA=""
case "$OSTYPE" in
  solaris*) APPDATA="~/" ;;
  darwin*)  APPDATA="$HOME/Library/Application Support" ;; 
  linux*)   APPDATA="~/" ;;
  bsd*)     APPDATA="~/" ;;
esac
if [ -d "${NODES_ROOT}" ] ; then
  rm -R "${NODES_ROOT}"
fi
if [ -d "${APPDATA}/Dcrpool/data/dcrpool.kv" ] ; then
  rm -R "${APPDATA}/Dcrpool/data/dcrpool.kv"
fi
mkdir -p "${NODES_ROOT}/"{master,pnode,pool,client}
cat > "${NODES_ROOT}/master.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
simnet=1
logdir=./log
datadir=./data
debuglevel=debug
txindex=true
listen=127.0.0.1:19555
EOF
cat > "${NODES_ROOT}/pnode.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
simnet=1
logdir=./log
datadir=./data
debuglevel=debug
connect=127.0.0.1:19555
listen=127.0.0.1:19565 
rpclisten=127.0.0.1:19566
txindex=1
miningaddr=${WALLET_MINING_ADDR}
EOF
cat > "${NODES_ROOT}/pool.conf" <<EOF
debuglevel=debug
homedir=${NODES_ROOT}/pool
rpchost=127.0.0.1:19566
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
rpccert=${APPDATA}/Dcrd/rpc.cert
port=:19576
miningaddr=${WALLET_MINING_ADDR}
EOF
cat > "${NODES_ROOT}/client.conf" <<EOF
debuglevel=debug
user=${MINING_USER}
pass=${MINING_PASS}
homedir=${NODES_ROOT}/client
host=127.0.0.1:19576
generate=1
EOF
cat > "${NODES_ROOT}/dcrctl.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
rpccert=${APPDATA}/Dcrd/rpc.cert
simnet=1
wallet=false
EOF

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION
################################################################################
# Setup the master node.
################################################################################
tmux rename-window -t $SESSION:1 'master'
tmux send-keys "cd ${NODES_ROOT}/master" C-m
tmux send-keys "dcrd -C ../master.conf " C-m
################################################################################
# Setup the pool node.
################################################################################
sleep 0.5
tmux new-window -t $SESSION:2 -n 'pnode'
tmux send-keys "cd ${NODES_ROOT}/pnode" C-m
tmux send-keys "dcrd -C ../pnode.conf " C-m
################################################################################
# Mine 32 blocks.
################################################################################
sleep 0.5
tmux new-window -t $SESSION:3 -n 'dcrctl'
tmux send-keys "dcrctl -C dcrctl.conf -s=127.0.0.1:19566 generate 32" C-m
################################################################################
# Setup the mining pool.
################################################################################
sleep 0.5
tmux new-window -t $SESSION:4 -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile ../pool.conf " C-m
################################################################################
# Setup the mining client.
################################################################################
sleep 3
tmux new-window -t $SESSION:5 -n 'client'
tmux send-keys "cd ${NODES_ROOT}/client" C-m
tmux send-keys `curl -s POST http://127.0.0.1:19576/create/account \
  -H 'Cache-Control: no-cache' \
  -H 'Content-Type: application/json' \
  -d '{"name": "pcl", \
  "address":"SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc" ,"pass": "pass"}' \
  > /dev/null`
tmux send-keys "dcrpclient --configfile ../client.conf " C-m

tmux attach-session -t $SESSION