package main

// ConfigFileContents is a string containing the commented example config for dcrpool.
const ConfigFileContents = `[Application Options]
; ------------------------------------------------------------------------------
; Debug settings
; ------------------------------------------------------------------------------
; Debug logging level.
; Valid levels are {trace, debug, info, warn, error, critical}
; You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set
; log level for individual subsystems.  Use dcrpool --debuglevel=show to 
; listavailable subsystems.
; debuglevel=

; ------------------------------------------------------------------------------
; Data settings
; ------------------------------------------------------------------------------
; The home directory of dcrpool.
; homedir=

; The directory to store data, dcrpool must keep record of registered
; users and only accept requests from miners authenticated by these users.  
; datadir=

; The config file directory.  
; configfile=

; The log file directory.  
; logdir=

; ------------------------------------------------------------------------------
; DB settings
; ------------------------------------------------------------------------------
; The database file.  
; dbfile=

; ------------------------------------------------------------------------------
; TLS settings
; ------------------------------------------------------------------------------
; The TLS certificate.
; tlscert=

; The TLS private key.
; tlskey=

; ------------------------------------------------------------------------------
; RPC settings
; ------------------------------------------------------------------------------
; The username and password to authenticate to a dcrd RPC server, dcrpool must 
; listen in on block template regenerations and update connected miners
; accordingly. 
; rpcuser=
; rpcpass=

; The path to the dcrd RPC certificate file.
; rpccert=

; ------------------------------------------------------------------------------
; Network settings
; ------------------------------------------------------------------------------
; The listening port for incoming requests.  
; port=

; ------------------------------------------------------------------------------
; Mining settings
; ------------------------------------------------------------------------------
; An address to pay mining subsidy for mined blocks to.
; miningaddr=
`
