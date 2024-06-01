module github.com/decred/dcrpool

go 1.21

toolchain go1.22.3

require (
	decred.org/dcrwallet/v4 v4.1.0
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.1
	github.com/decred/dcrd/certgen v1.1.3
	github.com/decred/dcrd/chaincfg/chainhash v1.0.4
	github.com/decred/dcrd/chaincfg/v3 v3.2.1
	github.com/decred/dcrd/crypto/blake256 v1.0.1
	github.com/decred/dcrd/dcrjson/v4 v4.1.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.2
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.3.0
	github.com/decred/dcrd/rpcclient/v8 v8.0.1
	github.com/decred/dcrd/txscript/v4 v4.1.1
	github.com/decred/dcrd/wire v1.7.0
	github.com/decred/slog v1.2.0
	github.com/gorilla/csrf v1.7.2
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/sessions v1.2.2
	github.com/gorilla/websocket v1.5.1
	github.com/jessevdk/go-flags v1.5.0
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.10.9
	go.etcd.io/bbolt v1.3.10
	golang.org/x/crypto v0.23.0
	golang.org/x/time v0.5.0
	google.golang.org/grpc v1.45.0
)

require (
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/decred/base58 v1.0.5 // indirect
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.1 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.2 // indirect
	github.com/decred/dcrd/dcrec v1.0.1 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0 // indirect
	github.com/decred/dcrd/gcs/v4 v4.1.0 // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	lukechampine.com/blake3 v1.3.0 // indirect
)
