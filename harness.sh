#!/bin/bash
# Tmux script that sets up a simnet mining harness.
set -e
SESSION="harness"
NODES_ROOT=~/harness
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
GUI_DIR="${NODES_ROOT}/gui"
CPU_MINING_ADDR="SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y"
POOL_MINING_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
PFEE_ADDR="SsVPfV8yoMu7AvF5fGjxTGmQ57pGkaY6n8z"
CLIENT_ADDRS=(
	"SsZckVrqHRBtvhJA5UqLZ3MDXpZHi5mK6uU"
	"Ssn23a3rJaCUxjqXiVSNwU6FxV45sLkiFpz"
	"Ssb7Peny8omFwicUi8hC6hCALhcEp59p7UK"
	"Ssc89aSDZpWXgYXptmzQyHaEikgiWhQhejo"
	"SsUkfDeV74D8TQ1Rr7EJwF2PiHfYoQ6dJmX"
	"SsXZ7SsV7Mk5UJSxAayFRjwxGSVmurgdgwM"
	"SscVXEvpU8havFgFyu1GZS9Kc5CSfHU7ajZ"
	"Ssj6Sd54j11JM8qpenCwfwnKD73dsjm68ru"
	"SssPc1UNr8czcP3W9hfAgpmLRa3zJPDhfSy"
	"Sshfmpt6YhQutEFTEXd4EWr3uQXtUgtcSka"
	"SspKX9jW5mRj33xNYdzBoXckKZTFh484JeG"
	"SspdbtJX8h4u6HriPVWyxv9VMxgfbFtvug3"
	"SssSPTMACV1pGNbNG4ueRgrzcbtqrCeKX7b"
	"SsmPRqFBqFoBHH58dHGA6yUSiWfUoKWfRQD"
	"SsVcnoy65By57gH2YFdD5Bt39EpdfvLoxMf"
	"SskyyHdnfhM8WDPQ6NqSr212QPoMgTwurGC"
	"SsXR8YkqDfE2Dsmvu39ygbLj5ojNSwimAP9"
	"Ssg6TcnCSAeRr1pZbEaCEecsr5V16xdyJgn"
	"SsfPSNgM2Hg7LksXoxsznLkfQrmp9sqffJ3"
	"SsXiquZP1exsqzC8YeGHZJUL2S9y6w5v6Vr"
)

# Number of mining clients to create. Maximum is determined by number of client
# addresses above - currently 20.
NUMBER_OF_CLIENTS=2

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

cp -r gui/assets ${GUI_DIR}/assets

for ((i = 0; i < $NUMBER_OF_CLIENTS; i++)); do
PROFILE_PORT=$(($i + 6061))
mkdir -p "${NODES_ROOT}/c$i"
cat > "${NODES_ROOT}/c$i/client.conf" <<EOF
debuglevel=trace
activenet=simnet
user=m$i
address=${CLIENT_ADDRS[$i]}
pool=127.0.0.1:5550
maxprocs=$MINER_MAX_PROCS
profile=:$PROFILE_PORT
EOF
done

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
adminpass=${ADMIN_PASS}
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
ticketbuyer.limit=10
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

tmux rename-window -t $SESSION 'master'
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

tmux new-window -t $SESSION -n 'mctl'
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

tmux new-window -t $SESSION -n 'mwallet'
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
tmux new-window -t $SESSION -n 'mwctl'
tmux send-keys "cd ${NODES_ROOT}/mwallet" C-m
tmux send-keys "./ctl createnewaccount pfee" C-m
tmux send-keys "./ctl getnewaddress pfee" C-m
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

tmux new-window -t $SESSION -n 'vnode'
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

tmux new-window -t $SESSION -n 'vctl'
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

tmux new-window -t $SESSION -n 'vwallet'
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
tmux new-window -t $SESSION -n 'vwctl'
tmux send-keys "cd ${NODES_ROOT}/vwallet" C-m

################################################################################
# Setup dcrpool.
################################################################################
echo "Starting dcrpool"
sleep 5
tmux new-window -t $SESSION -n 'pool'
tmux send-keys "cd ${NODES_ROOT}/pool" C-m
tmux send-keys "dcrpool --configfile=pool.conf --homedir=${NODES_ROOT}/pool" C-m

################################################################################
# Setup the mining clients.
################################################################################
for ((i = 0; i < $NUMBER_OF_CLIENTS; i++)); do
  echo "Starting mining client $i"
  sleep 1
  tmux new-window -t $SESSION -n c$i
  tmux send-keys "cd ${NODES_ROOT}/c$i" C-m
  tmux send-keys "miner --configfile=client.conf --homedir=${NODES_ROOT}/c$i" C-m
done

tmux attach-session -t $SESSION
