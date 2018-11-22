# Tmux script that sets up a mining harness. 
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

mkdir -p "${NODES_ROOT}/"{master,wallet,pool,client}
cat > "${NODES_ROOT}/dcrd.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
rpccert=${DCRD_RPC_CERT}
simnet=1
logdir=./log
datadir=./data
debuglevel=TXMP=debug,MINR=debug,BMGR=debug
miningaddr=${WALLET_MINING_ADDR}
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
rpccert=${DCRW_RPC_CERT}
rpckey=${DCRW_RPC_KEY}
logdir=./log
appdata=./data
pass=123
enablevoting=1
enableticketbuyer=1
ticketbuyer.nospreadticketpurchases=1
ticketbuyer.maxperblock=5
debuglevel=debug
EOF

cat > "${NODES_ROOT}/pool.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
dcrdrpchost=127.0.0.1:19556
walletgrpchost=127.0.0.1:19558
debuglevel=debug
homedir=.
port=:19560
activenet=simnet
poolfeeaddrs=${WALLET_MINING_ADDR}
EOF

cat > "${NODES_ROOT}/client.conf" <<EOF
debuglevel=debug
user=${MINING_USER}
pass=${MINING_PASS}
homedir=.
host=127.0.0.1:19560
minertype=cpu
EOF

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION

################################################################################
# Setup the node.
################################################################################
cat > "${NODES_ROOT}/master/mine" <<EOF
#!/bin/zsh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ../dcrctl.conf generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/master/mine"

cat > "${NODES_ROOT}/master/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/master/ctl"

tmux rename-window -t $SESSION:1 'master'
tmux send-keys "cd ${NODES_ROOT}/master" C-m
tmux send-keys "dcrd -C ../dcrd.conf" C-m

################################################################################
# Setup the dctl.
################################################################################
tmux new-window -t $SESSION:2 -n 'dctl'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

# mine the first block, this circumvents a wallet bug which occurs when the 
# wallet is connected before block 1 is mined.
tmux send-keys "./mine 1" C-m

################################################################################
# Setup the wallet.
################################################################################
sleep 5
cat > "${NODES_ROOT}/wallet/ctl" <<EOF
#!/bin/zsh
dcrctl -C ../dcrctl.conf --wallet \$*
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
# Setup wctl.
################################################################################
tmux new-window -t $SESSION:4 -n 'wctl'
tmux send-keys "cd ${NODES_ROOT}/wallet" C-m
tmux send-keys "./ctl getbalance"

################################################################################
# Setup dcrpool.
################################################################################
tmux new-window -t $SESSION:5 -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile ../pool.conf " C-m

################################################################################
# Setup the mining client.
################################################################################
sleep 5
tmux new-window -t $SESSION:6 -n 'client'
tmux send-keys "cd ${NODES_ROOT}/client" C-m
tmux send-keys `curl -s POST http://127.0.0.1:19560/create/account \
  -H 'Cache-Control: no-cache' \
  -H 'Content-Type: application/json' \
  -d '{"name": "pcl", \ 
  "address":"SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc" ,"pass": "pass"}' \
  > /dev/null`
tmux send-keys "poolclient --configfile ../client.conf " C-m

tmux attach-session -t $SESSION