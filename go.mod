module dnldd/dcrpool

require (
	github.com/boltdb/bolt v1.3.1
	github.com/coreos/bbolt v1.3.0
	github.com/decred/dcrd/blockchain v1.0.1
	github.com/decred/dcrd/dcrutil v1.1.1
	github.com/decred/dcrd/rpcclient v1.0.2
	github.com/decred/dcrd/wire v1.1.0
	github.com/decred/slog v1.0.0
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/handlers v1.4.0
	github.com/gorilla/mux v1.6.2
	github.com/gorilla/websocket v1.4.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/segmentio/ksuid v1.0.2
	golang.org/x/crypto v0.0.0-20181001203147-e3636079e1a4
	golang.org/x/sys v0.0.0-20181005133103-4497e2df6f9e // indirect
	golang.org/x/time v0.0.0-20180412165947-fbb02b2291d2
)

replace github.com/decred/dcrd/blockchain => ../dcrd/blockchain

replace github.com/decred/dcrd/dcrutil => ../dcrd/dcrutil

replace github.com/decred/dcrd/rpcclient => ../dcrd/rpcclient

replace github.com/decred/dcrd/wire => ../dcrd/wire

replace github.com/decred/dcrd/dcrjson => ../dcrd/dcrjson
