module github.com/decred/dcrpool

go 1.12

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/base58 v1.0.2 // indirect
	github.com/decred/dcrd/blockchain/standalone v1.1.0
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/mempool/v3 v3.1.0
	github.com/decred/dcrd/rpcclient/v5 v5.0.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrwallet/rpc/walletrpc v0.3.0
	github.com/decred/dcrwallet/wallet/v3 v3.1.0
	github.com/decred/slog v1.0.0
	github.com/gorilla/csrf v1.6.2
	github.com/gorilla/mux v1.7.3
	github.com/gorilla/sessions v1.2.0
	github.com/gorilla/websocket v1.4.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	go.etcd.io/bbolt v1.3.4
	golang.org/x/crypto v0.0.0-20191206172530-e9b2fee46413
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0
	google.golang.org/grpc v1.26.0
)
