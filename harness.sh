#!/bin/sh
# Tmux script that creates a cpu mining test harness with 2 dcrd nodes, 
# a dcrpool instance and a poolclient instance.
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
WALLET_POOL_FEE_ADDR="Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS" # NOTE: This must be changed if the seed is changed.
WALLET_XFER_ADDR="Sso52TPnorVkSaRYzHmi4FgU8F5BFEDZsiK" # same as above
APPDATA=""
case "$OSTYPE" in
  solaris*) APPDATA="${HOME}" ;;
  darwin*)  APPDATA="${HOME}/Library/Application Support" ;; 
  linux*)   APPDATA="${HOME}" ;;
  bsd*)     APPDATA="${HOME}" ;;
esac

DCRD_RPC_CERT="${APPDATA}/Dcrd/rpc.cert"
DCRW_RPC_CERT="${APPDATA}/Dcrwallet/rpc.cert"
DCRW_RPC_KEY="${APPDATA}/Dcrwallet/rpc.key"

if [ -d "${NODES_ROOT}" ] ; then
  rm -R "${NODES_ROOT}"
fi
if [ -d "${APPDATA}/Dcrpool/data/dcrpool.kv" ] ; then
  rm -R "${APPDATA}/Dcrpool/data/dcrpool.kv"
fi
mkdir -p "${NODES_ROOT}/"{master,pnode,wallet,pool,client}
cat > "${NODES_ROOT}/dcrd.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
rpccert=${DCRD_RPC_CERT}
simnet=1
logdir=./log
datadir=./data
debuglevel=debug
listen=127.0.0.1:19555
EOF
cat > "${NODES_ROOT}/pool.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
dcrdrpchost=127.0.0.1:19566
walletgrpchost=127.0.0.1:19558
debuglevel=debug
homedir=.
port=:19576
activenet=simnet
poolfeeaddrs=${WALLET_POOL_FEE_ADDR}
EOF
cat > "${NODES_ROOT}/client.conf" <<EOF
debuglevel=debug
user=${MINING_USER}
pass=${MINING_PASS}
homedir=.
host=127.0.0.1:19576
minertype=cpu
EOF
cat > "${NODES_ROOT}/dcrctl.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
simnet=1
EOF
cat > "${NODES_ROOT}/wallet.conf" <<EOF
username=${RPCUSER}
password=${RPCPASS}
simnet=1
logdir=./log
appdata=./data
cafile=${DCRD_RPC_CERT}
rpccert=${DCRW_RPC_CERT}
rpckey=${DCRW_RPC_KEY}
rpcconnect=127.0.0.1:19566
pass=123
enablevoting=1
enableticketbuyer=1
EOF
cd ${NODES_ROOT} && tmux new-session -d -s $SESSION
################################################################################
# Setup the master node.
################################################################################
tmux rename-window -t $SESSION:1 'master'
tmux send-keys "cd ${NODES_ROOT}/master" C-m
tmux send-keys "dcrd -C ../dcrd.conf " C-m
################################################################################
# Setup the pool node.
################################################################################
cat > "${NODES_ROOT}/pnode/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf -s=127.0.0.1:19566 \$*
EOF
chmod +x "${NODES_ROOT}/pnode/ctl"

tmux new-window -t $SESSION:2 -n 'pnode'
tmux send-keys "cd ${NODES_ROOT}/pnode" C-m
tmux send-keys "dcrd -C ../dcrd.conf --connect=127.0.0.1:19555 --listen=127.0.0.1:19565 --rpclisten=127.0.0.1:19566 --miningaddr=${WALLET_MINING_ADDR}" C-m
################################################################################
# Setup the wallet.
################################################################################
cat > "${NODES_ROOT}/wallet/ctl" <<EOF
#!/bin/sh
dcrctl -C ../dcrctl.conf --wallet -c "${DCRW_RPC_CERT}" \$*
EOF
chmod +x "${NODES_ROOT}/wallet/ctl"

tmux new-window -t $SESSION:3 -n 'wallet'
tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
tmux send-keys "dcrwallet -C ../wallet.conf --create" C-m
sleep 2
tmux send-keys "123" C-m "123" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C ../wallet.conf" C-m
################################################################################
# Setup dctl and wctl.
################################################################################
tmux new-window -t $SESSION:4 -n 'dctl'
tmux send-keys "cd ${NODES_ROOT}/pnode" C-m
sleep 3
tmux send-keys "./ctl generate 32" C-m

tmux new-window -t $SESSION:5 -n 'wctl'
tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
sleep 3
################################################################################
# Setup the mining pool.
################################################################################
sleep 0.5
tmux new-window -t $SESSION:6 -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile ../pool.conf " C-m
################################################################################
# Setup the mining client.
################################################################################
sleep 3
tmux new-window -t $SESSION:7 -n 'client'
tmux send-keys "cd ${NODES_ROOT}/client" C-m
tmux send-keys `curl -s POST http://127.0.0.1:19576/create/account \
  -H 'Cache-Control: no-cache' \
  -H 'Content-Type: application/json' \
  -d '{"name": "pcl", \ 
  "address":"Sso52TPnorVkSaRYzHmi4FgU8F5BFEDZsiK" ,"pass": "pass"}' \
  > /dev/null`
tmux send-keys "poolclient --configfile ../client.conf " C-m

tmux attach-session -t $SESSION