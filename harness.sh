#!/bin/sh
# Tmux script that sets up a simnet mining harness.
set -e
SESSION="harness"
NODES_ROOT=~/harness
RPC_USER="user"
RPC_PASS="pass"
MASTER_WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
VOTING_WALLET_SEED="aabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbc"
WALLET_PASS=123
BACKUP_PASS=b@ckUp
SOLO_POOL=0
MAX_GEN_TIME=20
MINER_MAX_PROCS=1
PAYMENT_METHOD="pplns"
LAST_N_PERIOD=300 # PPLNS range, 5 minutes.
GUI_DIR="${NODES_ROOT}/gui"
CPU_MINING_ADDR="SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y"
POOL_MINING_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
PFEE_ADDR="SsVPfV8yoMu7AvF5fGjxTGmQ57pGkaY6n8z"
CLIENT_ONE_ADDR="SsZckVrqHRBtvhJA5UqLZ3MDXpZHi5mK6uU"
CLIENT_TWO_ADDR="Ssn23a3rJaCUxjqXiVSNwU6FxV45sLkiFpz"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

echo "Writing node config files"
mkdir -p "${NODES_ROOT}/master"
mkdir -p "${NODES_ROOT}/vnode"
mkdir -p "${NODES_ROOT}/mwallet"
mkdir -p "${NODES_ROOT}/vwallet"
mkdir -p "${NODES_ROOT}/pool"
mkdir -p "${NODES_ROOT}/gui"
mkdir -p "${NODES_ROOT}/c1"
mkdir -p "${NODES_ROOT}/c2"

cp -r gui/assets ${GUI_DIR}/assets

cat > "${NODES_ROOT}/c1/client.conf" <<EOF
debuglevel=trace
activenet=simnet
user=m1
address=${CLIENT_ONE_ADDR}
pool=127.0.0.1:5550
maxprocs=${MINER_MAX_PROCS}
profile=:6061
EOF

cat > "${NODES_ROOT}/c2/client.conf" <<EOF
debuglevel=trace
activenet=simnet
user=m2
address=${CLIENT_TWO_ADDR}
pool=127.0.0.1:5550
maxprocs=${MINER_MAX_PROCS}
profile=:6062
EOF

cat > "${NODES_ROOT}/master/dcrmctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/master/rpc.cert
rpcserver=127.0.0.1:19556
EOF

cat > "${NODES_ROOT}/vnode/dcrvctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/vnode/rpc.cert
rpcserver=127.0.0.1:19560
EOF

cat > "${NODES_ROOT}/pool/pool.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:19556
dcrdrpccert=${NODES_ROOT}/master/rpc.cert
walletgrpchost=127.0.0.1:19558
walletrpccert=${NODES_ROOT}/mwallet/rpc.cert
debuglevel=trace
maxgentime=${MAX_GEN_TIME}
solopool=${SOLO_POOL}
activenet=simnet
walletpass=${WALLET_PASS}
poolfeeaddrs=${PFEE_ADDR}
paymentmethod=${PAYMENT_METHOD}
lastnperiod=${LAST_N_PERIOD}
backuppass=${BACKUP_PASS}
guidir=${GUI_DIR}
designation=${SESSION}
profile=:6060
EOF

cat > "${NODES_ROOT}/mwallet/dcrmwctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/mwallet/rpc.cert
rpcserver=127.0.0.1:19557
EOF

cat > "${NODES_ROOT}/vwallet/dcrvwctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/vwallet/rpc.cert
rpcserver=127.0.0.1:19562
EOF

cat > "${NODES_ROOT}/mwallet/mwallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${NODES_ROOT}/master/rpc.cert
logdir=${NODES_ROOT}/mwallet/log
appdata=${NODES_ROOT}/mwallet
simnet=1
pass=${WALLET_PASS}
EOF

cat > "${NODES_ROOT}/vwallet/vwallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${NODES_ROOT}/vnode/rpc.cert
logdir=${NODES_ROOT}/vwallet/log
appdata=${NODES_ROOT}/vwallet
simnet=1
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=4
pass=${WALLET_PASS}
rpcconnect=127.0.0.1:19560
grpclisten=127.0.0.1:19561
rpclisten=127.0.0.1:19562
EOF

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION

################################################################################
# Setup the master node.
################################################################################
cat > "${NODES_ROOT}/master/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrmctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/master/ctl"

tmux rename-window -t $SESSION:0 'master'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

echo "Starting simnet master node"
tmux send-keys "dcrd --appdata=${NODES_ROOT}/master \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR} \
--txindex \
--debuglevel=info \
--simnet" C-m

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
    dcrctl -C dcrmctl.conf generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/master/mine"

tmux new-window -t $SESSION:1 -n 'mctl'
tmux send-keys "cd ${NODES_ROOT}/master" C-m

sleep 3
# mine some blocks to start the chain.
tmux send-keys "./mine 2" C-m
echo "Mined 2 blocks"
sleep 1

tmux send-keys "./ctl livetickets"

################################################################################
# Setup the pool wallet.
################################################################################
cat > "${NODES_ROOT}/mwallet/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrmwctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/mwallet/ctl"

tmux new-window -t $SESSION:2 -n 'mwallet'
tmux send-keys "cd ${NODES_ROOT}/mwallet" C-m
tmux send-keys "dcrwallet -C mwallet.conf --create" C-m
echo "Creating simnet master wallet"
sleep 1
tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${MASTER_WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C mwallet.conf " C-m # --debuglevel=warn

# ################################################################################
# # Setup the pool wallet's dcrctl (wctl).
# ################################################################################
sleep 10
# The consensus daemon must be synced for account generation to 
# work as expected.
echo "Setting up pool wallet accounts"
tmux new-window -t $SESSION:3 -n 'mwctl'
tmux send-keys "cd ${NODES_ROOT}/mwallet" C-m
tmux send-keys "./ctl createnewaccount pfee" C-m
tmux send-keys "./ctl getnewaddress pfee" C-m
tmux send-keys "./ctl createnewaccount c1" C-m
tmux send-keys "./ctl getnewaddress c1" C-m
tmux send-keys "./ctl createnewaccount c2" C-m
tmux send-keys "./ctl getnewaddress c2" C-m
tmux send-keys "./ctl getnewaddress default" C-m
tmux send-keys "./ctl getbalance"

################################################################################
# Setup the voting node.
################################################################################
cat > "${NODES_ROOT}/vnode/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrvctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/vnode/ctl"

tmux new-window -t $SESSION:4 -n 'vnode'
tmux send-keys "cd ${NODES_ROOT}/vnode" C-m

echo "Starting simnet voting node"

tmux send-keys "dcrd --appdata=${NODES_ROOT}/vnode \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--connect=127.0.0.1:18555 \
--listen=127.0.0.1:19559 --rpclisten=127.0.0.1:19560 \
--miningaddr=${CPU_MINING_ADDR} \
--txindex \
--debuglevel=info \
--simnet" C-m

################################################################################
# Setup the voting node's dcrctl (vctl).
################################################################################
sleep 3
cat > "${NODES_ROOT}/vnode/mine" <<EOF
#!/bin/sh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C dcrvctl.conf generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/vnode/mine"

tmux new-window -t $SESSION:5 -n 'vctl'
tmux send-keys "cd ${NODES_ROOT}/vnode" C-m

tmux send-keys "./mine 30" C-m
sleep 10
echo "Mined 30 blocks, at stake enabled height (SEH)"

################################################################################
# Setup the voting wallet.
################################################################################
cat > "${NODES_ROOT}/vwallet/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrvwctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/vwallet/ctl"

cat > "${NODES_ROOT}/vwallet/tickets" <<EOF
#!/bin/sh
NUM=1
case \$1 in
    ''|*[!0-9]*) ;;
    *) NUM=\$1 ;;
esac
./ctl purchaseticket default 999999 1 \`./ctl getnewaddress\` \$NUM
EOF
chmod +x "${NODES_ROOT}/vwallet/tickets"

tmux new-window -t $SESSION:6 -n 'vwallet'
tmux send-keys "cd ${NODES_ROOT}/vwallet" C-m
tmux send-keys "dcrwallet -C vwallet.conf --create" C-m
echo "Creating simnet voting wallet"
sleep 1
tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${VOTING_WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C vwallet.conf --debuglevel=debug" C-m

################################################################################
# Setup the voting wallet's dcrctl (vwctl).
################################################################################
sleep 1
tmux new-window -t $SESSION:7 -n 'vwctl'
tmux send-keys "cd ${NODES_ROOT}/vwallet" C-m

################################################################################
# Setup dcrpool.
################################################################################
echo "Starting dcrpool"
sleep 5
tmux new-window -t $SESSION:8 -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile=pool.conf --homedir=${NODES_ROOT}/pool" C-m

################################################################################
# Setup first mining client. 
################################################################################
echo "Starting mining client 1"
sleep 1
tmux new-window -t $SESSION:9 -n 'c1'
tmux send-keys "cd ${NODES_ROOT}/c1" C-m
tmux send-keys "miner --configfile=client.conf --homedir=${NODES_ROOT}/c1" C-m

################################################################################
# Setup another mining client. 
################################################################################
echo "Starting mining client 2"
sleep 1
tmux new-window -t $SESSION:10 -n 'c2'
tmux send-keys "cd ${NODES_ROOT}/c2" C-m
tmux send-keys "miner --configfile=client.conf --homedir=${NODES_ROOT}/c2" C-m

tmux attach-session -t $SESSION