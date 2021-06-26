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

# set_default function gives the ability to move the setting of default # env variable from docker file to the script thereby giving the ability to the
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

# TODO? WALLETPASS, ADMINPASS, TLSCERT, TLSKEYS
PATH="/Users/${USERS}/Library/Application Support/Dcrpool"
# Set default variables if needed.
HOMEDIR=$(set_default "${HOMEDIR}" "/Users/${USERS}/Library/Application Support/Dcrpool")
CONFIG=$(set_default "${CONFIG}" "/Users/${USER}/Library/Application Support/Drpool/Dcrpool.conf")
DATADIR=$(set_default "${DATADIR}" "/Users/${USER}/Library/Application Support/Dcrpool/conf")
ACTIVENET=$(set_default "ACTIVENET" "simnet")
GUIPORT=$(set_default "$GUIPORT" "8080")
DEBUGLEVEL=$(set_default "$DEBUGLEVEL" "debug")
LOGDIR=$(set_default "${LOGDIR}" "/Users/${USERS}/Library/Application Support/Dcrpool/log")
DBFILE=$(set_default "${DBFILE}" "/Users/${USERS}/Library/Application Support/Dcrpool/dcrpool.kv")
POOLFEEADDRS=$(set_default "POOLFEEADDRS" "XXXXXXXX")
POOLFEE=$(set_default "$POOLFEE" "0.01")
MAXTXFEERESERVE=$(set_default "$MAXTXFEERESERVE" "0.1")
MAXGENTIME=$(set_default "$MAXGENTIME", "15s")
PAYMENTMETHODS=$(set_default "$PAYMENTMETHODS" "pplns")
LASTNPERIOD=$(set_default "${LASTNPERIOD}" "24H0m0s")
MINPAYMENT=$(set_default "${MINPAYMENT}" "0.2")
SOLOPOOL=$(set_default "${SOLOPOOL}" "0.2")
GUIDIR=$(set_default "${GUIDIR}" "gui")
DOMAIN=$(set_default "${DOMAIN}", "")
DESIGNATION=$(set_default "${DESIGNATION}" "decred-network")
MAXCONNPERHOST=$(set_default "${MAXCONNPERHOST}", "10")
PROFILE=$(set_default "${PROFILE}", "") # TODO: write in the correct default
CPUPORT=$(set_default "${CPUPORT}", "5550")
D9PORT=$(set_default "${D9PORT}", "5552")
DR3PORT=$(set_default "${DR3PORT}", "5553")
DR5PORT=$(set_default "${DR5PORT}", "5554")
D1PORT=$(set_default "${D1PORT}", "5555")
DCR1PORT=$(set_default "${DCR1PORT}", "5551")

PARAMS="--homedir=${HOMEDIR}" \ 
       "--configfile=${CONFIGFILE}" \ 
       "--activenet=${NETWORK}" \ 
       "--datadir=${PARAMS}" \
       "--activenet=${ACTIVENET}" \
       "--guiport=${GUIPORT}" \
       "--debuglevel=${DEBUGLEVEL}" \
       "--logdir=${LOGDIR}" \
       "--dbfile=${PATH}/data/dcrpool.kv" \
       "--poolfeeaddress=${POOLFEEADDRS}" \
       "--poolfee=${POOLFEE}" \
       "--maxtxfeereserve=${MAXTXFEERESERVE}" \
       "--maxgentime=${MAXGENTIME}" \
       "--paymentmethods=${PAYMENTMETHODS}" \
       "--lastnperiod=${LASTNPERIOD}" \
       "--minpayment=${MINPAYMENT}" \
       "--solopool=${SOLOPOOL}" \
       "--guidir=${GUIDIR}" \
       "--domain=${DOMAIN}" \
       "--uselehttps=${USELEHTTPS}" \
       "--tlscerts=${TLSCERTS}" \
       "--tlskeys=${TLSKEYS}" \
       "--designation=${DESIGNATION}" \
       "--maxconnperhost=${MAXCONNPERHOST}" \
       "--profile=${PROFILE}" \
       "--cpuport=${CPUPORT}" \
       "--d9port=${D9PORT}" \
       "--dr3port=${DR3PORT}" \
       "--dr5port=${DR5PORT}" \
       "--d1port=${D1PORT}" \
       "--dcr1port=${DCR1PORT}"

echo "running dcrpool"
./bin/dcrpool ${PARAMS}
