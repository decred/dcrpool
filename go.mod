module github.com/decred/dcrpool

go 1.13

require (
	decred.org/dcrwallet v1.6.0-rc1
	github.com/decred/dcrd/blockchain/standalone/v2 v2.0.0
	github.com/decred/dcrd/certgen v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrd/dcrjson/v3 v3.1.0
	github.com/decred/dcrd/dcrutil/v3 v3.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.3.0
	github.com/decred/dcrd/rpcclient/v6 v6.0.2
	github.com/decred/dcrd/wire v1.4.0
	github.com/decred/slog v1.1.0
	github.com/gorilla/csrf v1.7.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/sessions v1.2.1
	github.com/gorilla/websocket v1.4.2
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.9.0
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad
	golang.org/x/time v0.0.0-20201208040808-7e3f01d25324
	google.golang.org/grpc v1.34.0
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)
