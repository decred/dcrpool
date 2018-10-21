package main

// ConfigFileContents is a string containing the commented example config
// for dcrpclient.
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
; Mining account settings
; ------------------------------------------------------------------------------
; The mining account username and password.
; user=
; pass=

; ------------------------------------------------------------------------------
; CPU mining settings
; ------------------------------------------------------------------------------
; Enable built-in CPU mining.
;
; NOTE: This is typically only useful for testing purposes such as testnet or
; simnet since the difficulty on mainnet is far too high for CPU mining to be
; worth your while.
; generate=false

; ------------------------------------------------------------------------------
; Data settings
; ------------------------------------------------------------------------------
; The home directory of dcrpclient.
; homedir=

; The config file directory.  
; configfile=

; The log output directory.  
; logdir=

; ------------------------------------------------------------------------------
; Network settings
; ------------------------------------------------------------------------------
; The the IP address and port to connect to, in the form ip:port
; host=
`
