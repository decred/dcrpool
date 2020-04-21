#!/usr/bin/env bash

# exit from script if error was raised.
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

# Set default variables if needed.

# must have configuration values, the rest can be set as defaults
ADMINPASS=$(set_default "$ADMINPASS" "xxxx")
RPCUSER=$(set_default "$RPCUSER" "devuser")
RPCPASS=$(set_default "$RPCPASS" "devpass")
CONFIGFILE=$(set_default "$RPCPASS" "/data")
DATADIRECTORY=$(set_default "$DATADIRECTORY" "/data")

NETWORK=$(set_default "$NETWORK" "simnet")
NODESROOT=$(set_default "$NODESROOT" "nodesroot")
ADMINPASS=$(set_default "$ADMINPASS" "xxxx")
MAXGENTIME=$(set_default "$MAXGENTIME" "20s")

echo "writing configuration to ${DATADIRECTORY}/dcrpool.conf"
cat > "${DATADIRECTORY}/dcrpool.conf" <<EOF
adminpass=${ADMINPASS}
rpcuser=${RPCUSER}
rpcpass=${RPCPASS}

dcrdrpchost=127.0.0.1:19556
dcrdrpccert=${NODES_ROOT}/master/rpc.cert
walletgrpchost=127.0.0.1:19558
walletrpccert=${NODES_ROOT}/mwallet/rpc.cert
debuglevel=trace
maxgentime=${MAXGENTIME}
solopool=${SOLOPOOL}
activenet=${NET}
walletpass=${WALLETPASS}
poolfeeaddrs=${PFEEADDR}
paymentmethod=${PAYMENT_METHOD}
lastnperiod=${LASTNPERIOD}
adminpass=${ADMINPASS}
guidir=${GUIDIR}
designation=${SESSION}
profile=:6060
EOF
