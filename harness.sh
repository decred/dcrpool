#!/bin/bash
#
# Copyright (c) 2020 The Decred developers
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
#
# Tmux script that sets up a simnet mining harness.
#
# To use the script simply run `./harness.sh` from the repo root.
#
# The script makes a few assumptions about the system it is running on:
# - tmux is installed
# - dcrd, dcrwallet, dcrctl, miner and dcrpool are available on $PATH
# - /tmp directory exists

set -e

TMUX_SESSION="dcrpool-harness"
HARNESS_ROOT=/tmp/dcrpool-harness
RPC_USER="user"
RPC_PASS="pass"
MASTER_WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
VOTING_WALLET_SEED="aabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbc"
WALLET_PASS=123
ADMIN_PASS=aDm1N
SOLO_POOL=0
MAX_GEN_TIME=20s
MINER_MAX_PROCS=1
PAYMENT_METHOD="pplns"
LAST_N_PERIOD=5m
GUI_DIR="${HARNESS_ROOT}/gui"

# Using postgres requires the DB specified below to exist and contain no data.
USE_POSTGRES=true
POSTGRES_HOST=127.0.0.1
POSTGRES_PORT=5432
POSTGRES_USER=dcrpooluser
POSTGRES_PASS=12345
POSTGRES_DBNAME=dcrpooldb

# CPU_MINING_ADDR is the mining address printed during creation of vwallet.
# Initial block rewards from `generate` are sent here so vwallet can buy tickets.
CPU_MINING_ADDR="SsaJxXSymEGroxAiUY9u1mRq1DDWLxn5WhB"
# POOL_MINING_ADDR is the mining address printed during creation of mwallet.
# Block rewards from pool mining are sent here before being distributed to participants.
POOL_MINING_ADDR="SsXciQNTo3HuV5tX3yy4hXndRWgLMRVC7Ah"
# PFEE_ADDR is from mwallet, pfee account.
# Pool fees are collected here.
PFEE_ADDR="SsYcrXcBfUA1TAYiMhYRTkHupEapH7QWNU6"
# CLIENT_ADDRS are from mwallet, accounts c0, c1, etc.
# Client reward payments are sent to these addresses.
CLIENT_ADDRS=(
  "SscVdtfaNS1iCLceaT8Sqwd8hGoia8XDifR"
  "SsjUjmw8oGCg63C2MQPygFGZPh2Mbq3iaHM"
  "SsbpP2WFUWtnzm4GbiLUTVYWgpGiFDb6M8G"
  "SsVDptDDFQrfq7YZoDU3F1i6qNGgRo59FXG"
  "SsViw2X9sY74GfXjYm5ieWLZv3vNmmtZGXB"
  "SshjubSN3mXx6NTRiGnYZ2p9bU3ZKvMkd3f"
  "Ssj1cwFpr7JM1KqW9cePAXM6nXVYz7BHveC"
  "SsUgTPPZzQPQLq9MDYQKDXiAJjK4F5aQMjV"
  "Ssh37A184b5pequfjwZGSZrPyrAFJETg7c4"
  "Ssa9aqaHJYsPLyLRAz47j5uSJDmsSDkaSPU"
  "Ssjti72KzbRACaMQcjrXHEUHn374dm4hbn6"
  "SsUmjGtniYZxWQVkRUbN2Q65ou8poUMAFm8"
  "SsjqAzmWmPPKG7kk9LDs9ZNfi2y9HjkVXU6"
  "SsVwnUEBVSne61pqnxEU9WXh3HKeUemjHgU"
  "SsZpWMDW6UN8AZkKfW99RWG4imY93Bnu8X2"
  "SsqzmU32q7DWU8Pvqrhj7BLRZ4iy6wa6wTB"
  "SsWJKGHaEmTjN9AUrTPoLWbumBJGqZnGvz6"
  "SsbAAMn7b3Ei8rN4qq1CWZvgfytCHoSBq7c"
  "SsnL7yGMaASzaDVb3VVsVutWGzAMT8hHFvR"
  "SsjSDtCo8E6ztfib23kZSACFjW8Dfkv3obq"
)

# Number of mining clients to create. Maximum is determined by number of client
# addresses above - currently 20.
NUMBER_OF_CLIENTS=2

if [ -d "${HARNESS_ROOT}" ]; then
  rm -R "${HARNESS_ROOT}"
fi

echo "Writing node config files"
mkdir -p "${HARNESS_ROOT}/master"
mkdir -p "${HARNESS_ROOT}/vnode"
mkdir -p "${HARNESS_ROOT}/mwallet"
mkdir -p "${HARNESS_ROOT}/vwallet"
mkdir -p "${HARNESS_ROOT}/pool"
mkdir -p "${HARNESS_ROOT}/gui"

cp -r gui/assets ${GUI_DIR}/assets

for ((i = 0; i < $NUMBER_OF_CLIENTS; i++)); do
PROFILE_PORT=$(($i + 6061))
mkdir -p "${HARNESS_ROOT}/c$i"
cat > "${HARNESS_ROOT}/c$i/client.conf" <<EOF
debuglevel=trace
activenet=simnet
user=m$i
address=${CLIENT_ADDRS[$i]}
pool=127.0.0.1:5550
maxprocs=$MINER_MAX_PROCS
profile=$PROFILE_PORT
EOF
done

cat > "${HARNESS_ROOT}/master/dcrmctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${HARNESS_ROOT}/master/rpc.cert
rpcserver=127.0.0.1:19556
EOF

cat > "${HARNESS_ROOT}/vnode/dcrvctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${HARNESS_ROOT}/vnode/rpc.cert
rpcserver=127.0.0.1:19560
EOF

cat > "${HARNESS_ROOT}/pool/pool.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
dcrdrpchost=127.0.0.1:19556
dcrdrpccert=${HARNESS_ROOT}/master/rpc.cert
walletgrpchost=127.0.0.1:19558
walletrpccert=${HARNESS_ROOT}/mwallet/rpc.cert
debuglevel=trace
maxgentime=${MAX_GEN_TIME}
solopool=${SOLO_POOL}
activenet=simnet
walletpass=${WALLET_PASS}
poolfeeaddrs=${PFEE_ADDR}
paymentmethod=${PAYMENT_METHOD}
lastnperiod=${LAST_N_PERIOD}
adminpass=${ADMIN_PASS}
guidir=${GUI_DIR}
designation=${TMUX_SESSION}
profile=6060
postgres=${USE_POSTGRES}
postgreshost=${POSTGRES_HOST}
postgresport=${POSTGRES_PORT}
postgresuser=${POSTGRES_USER}
postgrespass=${POSTGRES_PASS}
postgresdbname=${POSTGRES_DBNAME}
EOF

cat > "${HARNESS_ROOT}/mwallet/dcrmwctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${HARNESS_ROOT}/mwallet/rpc.cert
rpcserver=127.0.0.1:19557
EOF

cat > "${HARNESS_ROOT}/vwallet/dcrvwctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${HARNESS_ROOT}/vwallet/rpc.cert
rpcserver=127.0.0.1:19562
EOF

cat > "${HARNESS_ROOT}/mwallet/mwallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${HARNESS_ROOT}/master/rpc.cert
clientcafile=${HARNESS_ROOT}/pool/wallet.cert
logdir=${HARNESS_ROOT}/mwallet/log
appdata=${HARNESS_ROOT}/mwallet
simnet=1
pass=${WALLET_PASS}
accountgaplimit=25
EOF

cat > "${HARNESS_ROOT}/vwallet/vwallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${HARNESS_ROOT}/vnode/rpc.cert
logdir=${HARNESS_ROOT}/vwallet/log
appdata=${HARNESS_ROOT}/vwallet
simnet=1
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=10
pass=${WALLET_PASS}
rpcconnect=127.0.0.1:19560
grpclisten=127.0.0.1:19561
rpclisten=127.0.0.1:19562
EOF

cd ${HARNESS_ROOT} && tmux new-session -d -s $TMUX_SESSION

################################################################################
# Setup the master node.
################################################################################
cat > "${HARNESS_ROOT}/master/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrmctl.conf \$*
EOF
chmod +x "${HARNESS_ROOT}/master/ctl"

tmux rename-window -t $TMUX_SESSION 'master'
tmux send-keys "cd ${HARNESS_ROOT}/master" C-m

echo "Starting simnet master node"
tmux send-keys "dcrd --appdata=${HARNESS_ROOT}/master \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR} \
--txindex --debuglevel=info \
--simnet" C-m

################################################################################
# Setup the master node's dcrctl (mctl).
################################################################################
cat > "${HARNESS_ROOT}/master/mine" <<EOF
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
chmod +x "${HARNESS_ROOT}/master/mine"

tmux new-window -t $TMUX_SESSION -n 'mctl'
tmux send-keys "cd ${HARNESS_ROOT}/master" C-m

sleep 1
# mine some blocks to start the chain.
tmux send-keys "./mine 2; tmux wait -S mine-two" C-m
tmux wait mine-two
echo "Mined 2 blocks"
tmux send-keys "./ctl livetickets"

################################################################################
# Setup the pool wallet.
################################################################################
cat > "${HARNESS_ROOT}/mwallet/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrmwctl.conf --wallet \$*
EOF
chmod +x "${HARNESS_ROOT}/mwallet/ctl"

tmux new-window -t $TMUX_SESSION -n 'mwallet'

# generate the needed dcrpool key pairs before starting the wallet.
tmux send-keys "cd ${HARNESS_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile=pool.conf --gencertsonly \
--homedir=${HARNESS_ROOT}/pool" C-m

tmux send-keys "cd ${HARNESS_ROOT}/mwallet" C-m
echo "Creating simnet master wallet"
tmux send-keys "dcrwallet -C mwallet.conf --create <<EOF
y
n
y
${MASTER_WALLET_SEED}
EOF" C-m
sleep 1
tmux send-keys "dcrwallet -C mwallet.conf --debuglevel=info" C-m

# ################################################################################
# # Setup the pool wallet's dcrctl (wctl).
# ################################################################################
sleep 6
# The consensus daemon must be synced for account generation to 
# work as expected.
echo "Setting up pool wallet accounts"
tmux new-window -t $TMUX_SESSION -n 'mwctl'
tmux send-keys "cd ${HARNESS_ROOT}/mwallet" C-m
tmux send-keys "./ctl createnewaccount pfee" C-m
tmux send-keys "./ctl getnewaddress pfee" C-m

# Create accounts & addresses for mining clients (only needed for debugging).
for ((i = 0; i < $NUMBER_OF_CLIENTS; i++)); do
  tmux send-keys "./ctl createnewaccount c$i" C-m
  tmux send-keys "./ctl getnewaddress c$i" C-m
done

tmux send-keys "./ctl getnewaddress default" C-m
tmux send-keys "./ctl getbalance"

################################################################################
# Setup the voting node.
################################################################################
cat > "${HARNESS_ROOT}/vnode/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrvctl.conf \$*
EOF
chmod +x "${HARNESS_ROOT}/vnode/ctl"

tmux new-window -t $TMUX_SESSION -n 'vnode'
tmux send-keys "cd ${HARNESS_ROOT}/vnode" C-m

echo "Starting simnet voting node"

tmux send-keys "dcrd --appdata=${HARNESS_ROOT}/vnode \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--connect=127.0.0.1:18555 \
--listen=127.0.0.1:19559 --rpclisten=127.0.0.1:19560 \
--miningaddr=${CPU_MINING_ADDR} \
--txindex --debuglevel=info \
--simnet" C-m

################################################################################
# Setup the voting node's dcrctl (vctl).
################################################################################
sleep 3
cat > "${HARNESS_ROOT}/vnode/mine" <<EOF
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
chmod +x "${HARNESS_ROOT}/vnode/mine"

tmux new-window -t $TMUX_SESSION -n 'vctl'
tmux send-keys "cd ${HARNESS_ROOT}/vnode" C-m

# mine to stake enabled height (SEH).
tmux send-keys "./mine 30; tmux wait -S mine" C-m
tmux wait mine

echo "Mined 30 blocks, at stake enabled height (SEH)"

################################################################################
# Setup the voting wallet.
################################################################################
cat > "${HARNESS_ROOT}/vwallet/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrvwctl.conf --wallet \$*
EOF
chmod +x "${HARNESS_ROOT}/vwallet/ctl"

cat > "${HARNESS_ROOT}/vwallet/tickets" <<EOF
#!/bin/sh
NUM=1
case \$1 in
    ''|*[!0-9]*) ;;
    *) NUM=\$1 ;;
esac
./ctl purchaseticket default 999999 1 \`./ctl getnewaddress\` \$NUM
EOF
chmod +x "${HARNESS_ROOT}/vwallet/tickets"

tmux new-window -t $TMUX_SESSION -n 'vwallet'
tmux send-keys "cd ${HARNESS_ROOT}/vwallet" C-m
echo "Creating simnet voting wallet"
tmux send-keys "dcrwallet -C vwallet.conf --create <<EOF
y
n
y
${VOTING_WALLET_SEED}
EOF" C-m
sleep 1
tmux send-keys "dcrwallet -C vwallet.conf --debuglevel=debug" C-m

################################################################################
# Setup the voting wallet's dcrctl (vwctl).
################################################################################
tmux new-window -t $TMUX_SESSION -n 'vwctl'
tmux send-keys "cd ${HARNESS_ROOT}/vwallet" C-m

################################################################################
# Setup dcrpool.
################################################################################
echo "Starting dcrpool"
sleep 5
tmux new-window -t $TMUX_SESSION -n 'pool'
tmux send-keys "cd ${HARNESS_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile=pool.conf --homedir=${HARNESS_ROOT}/pool" C-m

################################################################################
# Setup the mining clients.
################################################################################
for ((i = 0; i < $NUMBER_OF_CLIENTS; i++)); do
  echo "Starting mining client $i"
  sleep 1
  tmux new-window -t $TMUX_SESSION -n c$i
  tmux send-keys "cd ${HARNESS_ROOT}/c$i" C-m
  tmux send-keys "miner --configfile=client.conf --homedir=${HARNESS_ROOT}/c$i" C-m
done

tmux attach-session -t $TMUX_SESSION
