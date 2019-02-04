# Tmux script that sets up a mining harness. 
set -e
SESSION="harness"
NODES_ROOT=~/harness
RPCUSER="user"
RPCPASS="pass"
WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
POOL_MINING_ADDR="DsinLTk5vxzLUV6boLCKV8uQKCbWQ8gBu2Q"
PFEE_ADDR="DsinLTk5vxzLUV6boLCKV8uQKCbWQ8gBu2Q"
CLIENT_ADDR="DsinLTk5vxzLUV6boLCKV8uQKCbWQ8gBu2Q"

if [ -d "${NODES_ROOT}" ] ; then
  rm -R "${NODES_ROOT}"
fi

DCRD_RPC_CERT="${HOME}/.dcrd/rpc.cert"
DCRW_RPC_CERT="${HOME}/.dcrwallet/rpc.cert"
DCRW_RPC_KEY="${HOME}/.dcrwallet/rpc.key"

mkdir -p "${NODES_ROOT}/master"
mkdir -p "${NODES_ROOT}/wallet"
mkdir -p "${NODES_ROOT}/pool"
mkdir -p "${NODES_ROOT}/client"

cat > "${NODES_ROOT}/dcrctl.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
EOF

cat > "${NODES_ROOT}/wallet/wallet.conf" <<EOF
username=${RPCUSER}
password=${RPCPASS}
rpccert=${DCRW_RPC_CERT}
rpckey=${DCRW_RPC_KEY}
logdir=./log
appdata=./data
pass=123
debuglevel=debug
EOF

cat > "${NODES_ROOT}/pool/pool.conf" <<EOF
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}
dcrdrpchost=127.0.0.1:9109
walletgrpchost=127.0.0.1:19558
debuglevel=trace
maxgentime=5
solopool=1
walletpass=123
activenet=mainnet
poolfeeaddrs=${PFEE_ADDR}
paymentmethod=pplns
lastnperiod=10
; paymentmethod=pps
EOF

cat > "${NODES_ROOT}/client/client.conf" <<EOF
debuglevel=trace
activenet=mainnet
user=miner
address=${CLIENT_ADDR}
pool=127.0.0.1:5550
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
tmux send-keys "dcrd -C ${HOME}/dcrd.updated.conf" C-m

################################################################################
# Setup the master node's dcrctl (mctl).
################################################################################
sleep 2
tmux new-window -t $SESSION:2 -n 'mctl'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

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
tmux send-keys "dcrpool --configfile pool.conf --homedir=." C-m

################################################################################
# Setup the mining client. 
################################################################################
# sleep 1
# tmux new-window -t $SESSION:6 -n 'client'
# tmux send-keys "cd ${NODES_ROOT}/client" C-m
# tmux send-keys "miner --configfile client.conf --homedir=." C-m

tmux attach-session -t $SESSION