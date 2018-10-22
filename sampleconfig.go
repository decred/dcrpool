package main

// ConfigFileContents is a string containing the commented example config for
// dcrpclient.
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
; RPC settings
; ------------------------------------------------------------------------------
; The username and password to authenticate dcrd and wallet RPC servers.
; rpcuser=
; rpcpass=

; The ip:port to establish an RPC connection for dcrd.
; dcrdrpchost=

; The ip:port to establish a GRPC connection for the wallet.
; walletgrpchost=

; ------------------------------------------------------------------------------
; Network settings
; ------------------------------------------------------------------------------
; The listening port for incoming requests.  
; port=

; The active network {simnet, testnet, mainnet}.
; activenet=

; ------------------------------------------------------------------------------
; Mining settings
; ------------------------------------------------------------------------------
; An address to pay mining subsidy for mined blocks to.
; miningaddr=

; ------------------------------------------------------------------------------
; Pool settings
; ------------------------------------------------------------------------------
; The fee charged for pool participation.
; poolfee=

; The share creation target time for the pool in seconds.
; sharetime=

; The payment method of the pool {pps, pplns}.
; paymentmethod=

; The period of interest when using the PPLNS payment scheme.
; lastnperiod=

; The pool wallet's passphrase.
; walletpass=
`
