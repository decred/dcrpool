#!/bin/bash
# Tmux script that sets up a simnet mining harness.
set -e

# error function is used within a bash function in order to send the error
# message directly to the stderr output and exit.
error() {
    echo "$1" > /dev/stderr
    exit 0
}

# return is used within bash function in order to return the value.
return() {
    echo "$1"
}

# set_default function gives the ability to move the setting of default
# env variable from docker file to the script thereby giving the ability to the
# user override it durin container start.
set_default() {
    # docker initialized env variables with blank string and we can't just
    # use -z flag as usually.
    BLANK_STRING='""'

    VARIABLE="$1"
    DEFAULT="$2"

    if [[ -z "$VARIABLE" || "$VARIABLE" == "$BLANK_STRING" ]]; then

        if [ -z "$DEFAULT" ]; then
            error "You should specify default variable"
        else
            VARIABLE="$DEFAULT"
        fi
    fi

   return "$VARIABLE"
}

# set_input function takes in variables that either guide that harness or override set_defaultsvars
set_input() {
    if [ "$(($#%2))" -ne 0 ]; then
        echo "Wrong input must be an even pairs" 
        exit 1
    fi

    for ((i = 1; i < $# ; i++)); do
        if [ "$((i%2))" -ne 1 ]; then
            continue
        fi

        export "${@:i:1}"=${@:i+1:1}
    done
}

set_input "${@}"

SESSION=$(set_default "${SESSION}" "dcrpool_harness")
NODES_ROOT=$(set_default "${NODES_ROOT}" "~/dcrpool")
PROJECT_ROOT=$(set_default ${PROJECT_ROOT} $(cd .. && echo $(pwd)))
NUMBER_OF_CLIENTS=$(set_default "${NUMBER_OF_CLIENTS}" 1)

# remove an already existing harness file and remake it
if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

mkdir -p "${NODES_ROOT}"
tmux new-session -d -s $SESSION

# create a new session
if [[ ${POSTGRES_ONLY} == "true" ]]; then
    tmux rename-window -t $SESSION 'postgres'
fi

# override the postgres env vars if necessary
if [[ ${POSTGRES_DB} == "true" || ${POSTGRES_ONLY} == true ]]; then
    DBTYPE="postgres" POSTGRES_DATA=$(set_default "${POSTGRES_DATA}", "${NODES_ROOT}/sql/data")
    POSTGRES_LOGS=$(set_default "${POSTGRES_LOGS}", "${NODES_ROOT}/sql/logs/log")
    POSTGRES_PORT=$(set_default "${POSTGRES_PORT}", 5433)
    POSTGRES_HOST=$(set_default "${POSTGRES_HOST}" "localhost")
    POSTGRES_DEMO=$(set_default "${POSTGRES_DEMO}" "")
    POSTGRES_USER=$(set_default "${POSTGRES_USER}" $(whoami))
    POSTGRES_PASSWORD=$(set_default "${POSTGRES_PASS}" "pass")
fi

if [[ ${POSTGRES_DB} == "true" ]]; then
echo -e "\n
################################################################################
# Setup dcrpool's postgres database
################################################################################
"
# 1. cerate sql assets in noodes root
# 2. create cluster & start it using pg_ctl
# 3. create the db using createdb

echo "creating sql assets in ${NODES_ROOT}/sql/"
mkdir -p "${NODES_ROOT}/sql/"
touch "${NODES_ROOT}/sql/file.sql"
mkdir -p "${NODES_ROOT}/sql/data"
mkdir "${NODES_ROOT}/sql/logs/"
touch "${NODES_ROOT}/sql/logs/log"

echo "sql assets have been created"
chmod +x "${NODES_ROOT}/sql/file.sql"

# create cluster/data directory using pg_ctl
echo "creating the postgres cluster"
tmux send-keys -t "${SESSION}" " pg_ctl init \
    -D ${POSTGRES_DATA}" C-m

sleep 10s

echo "starting the postgres server"
tmux send-keys -t "${SESSION}" "pg_ctl start \
    -D ${POSTGRES_DATA} \
    -l ${POSTGRES_LOGS} \ 
    -o "-F -p "${POSTGRES_PORT}""" C-m
fi

sleep 3s

# start the postgres db
# tmux send-keys -t ${SESSION} "postgres" \
# -D ${POSTGRES_DATA} \

# createthedb
tmux send-keys -t ${SESSION} "createdb dcrpool \
    --tablespace="${POSTGRES_DATA}" \
    --template=template0 \
    -O "${POSTGRES_USER}" \
    -l "${POSTGRES_HOST}" \
    -p "${POSTGRES_PORT}" \
    -U "${POSTGRES_USER}" \
    "${POSTGRES_DEMO}"" C-m

if [[ ${POSTGRES_CLIENT} == "true" ]]; then
echo -e "\n
################################################################################
# Setup the postgres client
################################################################################

"
fi

if [[ ${POSTGRES_ONLY} == "true" ]]; then
    echo "postgres setup completion finished, exiting the harness"
    exit 0
fi

# TODO: if the rest of these want to be able to be overriden they need to be
# set the the set_default function
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
  "SsgGu2Fz3c2YeoRKZMeXNQBJ324J8uFe2ku"
  "Ssj65eTTHTvyEyQJBPuqUuueLouf5yMtmJL"
  "SsZKVJQnN3Hm3A1Ga3WTTZMRceeGTZgsMTQ"
  "SsivBg41hYAxGf4FDK8swGoxmJMTk8kqaks"
  "Ssi34kZ7HN9WNHkofWsjKajoYwiAdryfr89"
  "SsnEdBRWU5zVfo6rQxVkyVikCF2X3p5mVrW"
  "SskZsGb78uyvkzCF7aqHYa24oWWHLF3XEKe"
  "SsaJoAxcB3bTGoavzpymrx1q2wa6nkna35g"
  "SsUpkNXC5824166ASdw72BFE8zeF4i4XaDp"
  "SsZbyZp62wrEiZQ3iLyfUgpTg4KzNUNmzVP"
  "SsaYJ3DYpaxquCd2cdD6Zba8p6jTnBFVjck"
  "SsZpjNR3ZfrRzKGMtRZixoRb2uii43qE2QE"
  "SsWTW7sgp5Pb1Hede5imKDUP5ymYZTXkzkX"
  "SssRDNnKvD2bfKvKfC3p8b5UGR78nivxG56"
  "Ssmc27WaSfizoyvhy6GSht5XtC9DYswKyTD"
  "SsV83wxme92uY6tDKWxnGer5GBKXDHpknDo"
  "Sse8V9WrWLSHS5t4WEr2Cy92FGRsxGwXTfo"
  "SssSxnc6rixXPowbxcdrXg6PccAHFCe4x6K"
)

# remove an already existing harness file
if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

# depricated: configuratting clients should be done in the Makefile
# Number of mining clients to create. Maximum is determined by number of client
# addresses above - currently 20.
# NUMBER_OF_CLIENTS=2

echo -e "\n
################################################################################
# Writing node config files
################################################################################
\n"

mkdir -p "${NODES_ROOT}/master"
mkdir -p "${NODES_ROOT}/vnode"
mkdir -p "${NODES_ROOT}/mwallet"
mkdir -p "${NODES_ROOT}/vwallet"
mkdir -p "${NODES_ROOT}/pool"
mkdir -p "${NODES_ROOT}/gui"

echo "Writing config files in ${PROJECT_ROOT}"
cp -r ${PROJECT_ROOT}/gui/assets ${GUI_DIR}/assets

for ((i = 0; i < ${NUMBER_OF_CLIENTS}; i++)); do
PROFILE_PORT=$(($i + 6061))
mkdir -p "${NODES_ROOT}/c$i"
cat > "${NODES_ROOT}/c$i/client.conf" <<EOF
debuglevel=trace
activenet=simnet
user=m$i
address=${CLIENT_ADDRS[$i]}
pool=127.0.0.1:5550
maxprocs=$MINER_MAX_PROCS
profile=$PROFILE_PORT
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
profile=6060
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
accountgaplimit=25
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

echo -e "\n\n"

################################################################################
# Setup the master node.
################################################################################

cat > "${NODES_ROOT}/master/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrmctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/master/ctl"

tmux rename-window -t $SESSION 'master'
tmux send-keys -t "$SESSION:master" "cd ${NODES_ROOT}/master" C-m
tmux rename-window -t $SESSION 'master'
tmux send-keys -t "$SESSION:master" "dcrd --appdata=${NODES_ROOT}/master \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${POOL_MINING_ADDR} \
--txindex \
--debuglevel=info \
--simnet" C-m

echo -e "\n

################################################################################
# Setup the master node's dcrctl (mctl).
################################################################################
\n"
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
tmux send-keys -t "$SESSION:mctl" "cd ${NODES_ROOT}/master" C-m

sleep 3
# mine some blocks to start the chain.
tmux send-keys -t "$SESSION:mctl" "./mine 3" C-m
echo "Mined 2 blocks"
sleep 1

tmux send-keys -t "$SESSION:mctl" './ctl livetickets'

echo -e "\n
################################################################################
# Setup the pool wallet.
################################################################################
\n"
cat > "${NODES_ROOT}/mwallet/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrmwctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/mwallet/ctl"

tmux new-window -t $SESSION -n 'mwallet'
tmux send-keys -t "$SESSION:mwallet" "cd ${NODES_ROOT}/mwallet" C-m
tmux send-keys -t "$SESSION:mwallet" "dcrwallet -C mwallet.conf --create" C-m

echo "Creating simnet master wallet"
sleep 1
tmux send-keys -t "$SESSION:mwallet" "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys -t "$SESSION:mwallet" "${MASTER_WALLET_SEED}" C-m C-m
tmux send-keys -t "$SESSION:mwallet" "dcrwallet -C mwallet.conf " C-m # --debuglevel=warn

sleep 10
echo -e "\n Waiting on the consensus daemon must be synced for account generation to
work as expected.
"

echo -e "\n
# ################################################################################
# Setup the pool wallet's dcrctl (mwctl).
# ################################################################################
\n"
sleep 15
# The consensus daemon must be synced for account generation to 
# work as expected.

tmux new-window -t $SESSION -n 'mwctl'
tmux send-keys -t "$SESSION:mwctl" "cd ${NODES_ROOT}/mwallet" C-m
tmux send-keys -t "$SESSION:mwctl" "./ctl createnewaccount pfee" C-m
tmux send-keys -t "$SESSION:mwctl" "./ctl getnewaddress pfee" C-m

# Create accounts & addresses for mining clients (only needed for debugging).
echo -e "Creating accounts & addresses for ${NUMBER_OF_CLIENTS} mining clients\n"
for ((i = 0; i < ${NUMBER_OF_CLIENTS}; i++)); do
   echo "creating mining client $i"
  tmux send-keys -t "$SESSION:mwctl" "./ctl createnewaccount c$i" C-m
  tmux send-keys -t "$SESSION:mwctl" "./ctl getnewaddress c$i" C-m
  # "bugs out here"
done

tmux send-keys -t "$SESSION:mwctl" "./ctl getnewaddress default" C-m
tmux send-keys -t "$SESSION:mwctl" "./ctl getbalance" C-m

echo -e "\n
################################################################################
# Setup the voting node.
################################################################################
\n"
cat > "${NODES_ROOT}/vnode/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrvctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/vnode/ctl"

tmux new-window -t $SESSION -n 'vnode'
tmux send-keys -t "$SESSION:vnode" "cd ${NODES_ROOT}/vnode" C-m
tmux send-keys -t "${SESSION}:vnode" "dcrd --appdata=${NODES_ROOT}/vnode \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--connect=127.0.0.1:18555 \
--listen=127.0.0.1:19559 --rpclisten=127.0.0.1:19560 \
--miningaddr=${CPU_MINING_ADDR} \
--txindex \
--debuglevel=info \
--simnet" C-m
sleep 10

echo -e "\n
################################################################################
# Setup the voting node's dcrctl (vctl).
################################################################################
\n"
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
tmux send-keys -t "$SESSION:vctl" "cd ${NODES_ROOT}/vnode" C-m
tmux send-keys -t "$SESSION:vctl" "./mine 30" C-m
sleep 10
echo "Mined 30 blocks, at stake enabled height (SEH)"

echo -e "\n
################################################################################
# Setup the voting wallet.
################################################################################
\n"
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
tmux send-keys -t "$SESSION:vwallet" "cd ${NODES_ROOT}/vwallet" C-m
tmux send-keys -t "$SESSION:vwallet" "dcrwallet -C vwallet.conf --create" C-m
echo "Creating simnet voting wallet"
sleep 1
tmux send-keys -t "$SESSION:vwallet" "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys -t "$SESSION:vwallet" "${VOTING_WALLET_SEED}" C-m C-m
tmux send-keys -t "$SESSION:vwallet" "dcrwallet -C vwallet.conf --debuglevel=debug" C-m

echo -e "\n
################################################################################
# Setup the voting wallet's dcrctl (vwctl).
################################################################################
"
sleep 1
tmux new-window -t $SESSION -n 'vwctl'
tmux send-keys -t "$SESSION:vwctl" "cd ${NODES_ROOT}/vwallet" C-m
# TODO: nothing happens here, looks like no votes are made

echo -e "\n
################################################################################
# Setup dcrpool.
################################################################################
\n"
sleep 5
echo -e "\nSetting up the dcr pool\n"
tmux new-window -t $SESSION -n 'pool'
tmux send-keys -t "$SESSION:pool" "cd ${NODES_ROOT}/pool" C-m
tmux send-keys -t "$SESSION:pool" "dcrpool --dbtype=${DBTYPE} --configfile=pool.conf --homedir=${NODES_ROOT}/pool" C-m

echo -e "\n
################################################################################
# Setup the mining clients.
################################################################################
\n"


for ((i = 0; i < ${NUMBER_OF_CLIENTS}; i++)); do
  echo "Starting mining client $i"
  sleep 1
  tmux new-window -t $SESSION -n c$i
  tmux send-keys -t "$SESSION:c$i" "cd ${NODES_ROOT}/c$i" C-m
  tmux send-keys -t "$SESSION:c$i" "miner --configfile=client.conf --homedir=${NODES_ROOT}/c$i" C-m
done
# tmux attach-session -t $SESSION
