module dnldd/dcrpool

require (
	github.com/boltdb/bolt v1.3.1
	github.com/coreos/bbolt v1.3.0
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain v1.0.2
	github.com/decred/dcrd/dcrutil v1.1.1
	github.com/decred/dcrd/rpcclient v1.0.2
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrwallet/rpc/walletrpc v0.1.0
	github.com/decred/slog v1.0.0
	github.com/gorilla/handlers v1.4.0
	github.com/gorilla/mux v1.6.2
	github.com/gorilla/websocket v1.4.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/segmentio/ksuid v1.0.2
	golang.org/x/crypto v0.0.0-20181015023909-0c41d7ab0a0e
	golang.org/x/time v0.0.0-20180412165947-fbb02b2291d2
	google.golang.org/grpc v1.15.0
)

replace github.com/decred/dcrd/blockchain => ../dcrd/blockchain

replace github.com/decred/dcrd/dcrutil => ../dcrd/dcrutil

replace github.com/decred/dcrd/rpcclient => ../dcrd/rpcclient

replace github.com/decred/dcrd/wire => ../dcrd/wire

replace github.com/decred/dcrd/dcrjson => ../dcrd/dcrjson

replace github.com/decred/dcrd/chaincfg => ../dcrd/chaincfg
