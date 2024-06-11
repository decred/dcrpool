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
HOMEDIR=$(set_default "$HOMEDIE" "/data/dcrpool.conf")
CONFIG=$(set_default "$CONFIGFILE" "/data/dcrpool.conf")
USER=$(set_default "$USER" "user")
ADDRESS=$(set_default "$ADDRESS" "address") # this needs to be shared
POOL=$(set_default "$POOL" "pool") # THIS NEEDS TO BE SHARED
DEBUGLEVEL=$(set_default "$DEBUGLEVEL" "xxxx")
LOGDIR=$(set_default "$LOGDIR" "/${USER}/Library/Application Support/Miner/log")
MAXPROCS=$(set_default "$MAXPROCS" "1")
PROFILE=$(set_default "$PROFILE" "xxxx")
STALL=$(set_default "$STALL" "false")

PARAMS="--homedir=${HOMEDIR}" \
       "--configfile=${CONFIG}" \
       "--user=${USER}" \
       "--address=${ADDRESS}" \
       "--pool=${POOL}" \
       "--debuglevel=${DEBUGLEVEL}" \
       "--logdir=${LOGDIR}" \
       "--maxprocs=${MAXPROCS}" \
       "--profile=${PROFILE}" \
       "--stall=${STALL}" \

./bin/dcrpool ${PARAMS}
