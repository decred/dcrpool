# Using the Makefile:
#
# 1. Setting dcrpools database type:
#
# The default logic sets a number of clients and runs the harness with dcrpool db as a keystore
# to run dcrpool's database as a postgres run "make startup_postgres"
#
# If you need to install postgress run the
# "install_psql" recipe before running the startup_postgress make.
#
# 2. Setting mining pool clients:
#
# Also if require to change the number of mining clients change the miner number within the set_client recipe
#
# 3. Make in action
#
# EX1) install postgres and start the harness with a postgres db
#
#  make install_postgress
#  make startup_postgress
#
# EX2) start the harness with keyvalue store
#
#  make
#
# EX3) kill the dcrpool session within tmux and all its artifacts
#
#  make clean

OS_NAME := $(shell uname -s | tr A-Z a-z)

# override default harness values
# NODES_ROOT := ~/dcrpoolharness
NUMBER_OF_CLIENTS := 2

POSTGRES_DATA := $(NODES_ROOT)/sql/data
POSTGRES_LOG := $(NODES_ROOT)/sql/log
POSTGRES_DEMO := "-e demo"

POSTGRES_DBNAME := 5433
POSTGRES_HOST := localhost
POSTGRES_PORT := 5433
POSTGRES_USER := user

.DEFAULT_GOAL := startup
.PHONY: all clean

# harness startup defaults to a key value database and sets the number of
# clients
startup:
	exec ./harness.sh NUMBER_OF_CLIENTS $(NUMBER_OF_CLIENTS)

# startup_postgres_only starts a postgres db by itself without decred instances
startup_postgres_only:
	exec ./harness.sh \
		"POSTGRES_ONLY" "true" \
		"POSTGRES_DB" "true" \
		POSTGRES_DATA $(POSTGRES_DATA) \
		POSTGRES_LOG $(POSTGRES_LOG) \
		POSTGRES_DEMO $(POSTGRES_DEMO) \
		POSTGRES_HOST $(POSTGRES_HOST) \
		POSTGRES_PORT $(POSTGRES_PORT) \
		POSTGRES_PASS $(POSTGRES_USER) \
		POSTGRES_PASS $(POSTGRES_PASS)

# startup_postgres starts the harness with a postgres database
startup_postgres:
	exec ./harness.sh \
		"POSTGRES_DB" "true" \
		POSTGRES_DATA "data" \
		POSTGRES_LOG $(POSTGRES_PORT) \
		POSTGRES_DEMO $(POSTGRES_DEMO) \
		POSTGRES_HOST $(POSTGRES_HOST) \
		POSTGRES_PORT $(POSTGRES_PORT) \
		NUMBER_OF_CLIENTS $(NUMBER_OF_CLIENTS)

# statup postgres_with_client starts the harness with a postgres database and
# a psql client.  this is useful for testing the postgres db on the non application level 
startup_postgres_with_client:
	exec ./harness.sh \
		"POSTGRES_DB true" \
		"POSTGRES_CLIENT true" \
		POSTGRES_DATA $(POSTGRES_DATA) \
		POSTGRES_LOG $(POSTGRES_LOG) \
		POSTGRES_DEMO $(POSTGRES_DEMO) \
		POSTGRES_HOST $(POSTGRES_HOST) \
		POSTGRES_PORT $(POSTGRES_PORT) \
		NUMBER_OF_CLIENTS $(NUMBER_OF_CLIENTS)

# check_postgres checks if the database is ready
check_postgres:
	pg_isready \
		--dbname=$(POSTGRES_DBNAME) \
		--host=$(POSTGRES_HOST) \
		--port=$(POSTGRES_PORT) \
		--username=$(POSTGRES_USER) \
		#--password=$(POSTGRES_PASSWORD)

# send command sends a postgres command to the given server
sendcmd_postgres:
	@echo "what command do you want to execute?"
	read FILE;
	psql \
		--file=$(FILE) \
		--hostname=$(POSTGRES_HOST) \
		--port=$(POSTGRES_PORT) \
		--username=$(POSTGRES_USERNAME) \
		--password=$(POSTGRES_PASSWORD)

restart_postgres:
	pg_ctl -w restart -D $(POSTGRES_DATA) \

status_postgres:
	pg_ctl status

# install postgres installs postgres given on your machine's OS
install_postgres:
ifeq ($(OS_NAME), "darwin")
	brew install postgressql
endif
ifeq ($(OS_NAME), "linux")
	apt-get postgressql
endif

# clean cleans up the harness' artifacts
clean:
	@echo "cleaning up the harness"
	tmux kill-session -t dcrpool_harness
	rm -rf ~/dcrpoolharness
	clear
