module github.com/decred/dcrpool

go 1.12

require (
	github.com/decred/dcrd/blockchain/standalone v1.1.0
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrd/dcrjson/v3 v3.0.1
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.0.0
	github.com/decred/dcrd/rpcclient/v5 v5.0.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrwallet/rpc/walletrpc v0.3.0
	github.com/decred/dcrwallet/wallet/v3 v3.2.1
	github.com/decred/slog v1.0.0
	github.com/gorilla/csrf v1.7.0
	github.com/gorilla/mux v1.7.4
	github.com/gorilla/sessions v1.2.0
	github.com/gorilla/websocket v1.4.2
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	go.etcd.io/bbolt v1.3.4
	golang.org/x/crypto v0.0.0-20200510223506-06a226fb4e37
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1
	google.golang.org/grpc v1.29.1
)
