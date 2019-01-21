module github.com/dnldd/dcrpool

require (
	github.com/coreos/bbolt v1.3.0
	github.com/davecgh/go-spew v1.1.0
	github.com/decred/dcrd/blockchain v1.1.1
	github.com/decred/dcrd/chaincfg v1.2.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrutil v1.2.0
	github.com/decred/dcrd/rpcclient v1.1.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrwallet/rpc/walletrpc v0.2.0
	github.com/decred/slog v1.0.0
	github.com/google/pprof v0.0.0-20181206194817-3ea8567a2e57 // indirect
	github.com/gorilla/handlers v1.4.0
	github.com/gorilla/mux v1.6.2
	github.com/gorilla/websocket v1.4.0
	github.com/ianlancetaylor/demangle v0.0.0-20181102032728-5e5cf60278f6 // indirect
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	golang.org/x/arch v0.0.0-20181203225421-5a4828bb7045 // indirect
	golang.org/x/crypto v0.0.0-20181203042331-505ab145d0a9
	golang.org/x/time v0.0.0-20181108054448-85acf8d2951c
	google.golang.org/grpc v1.17.0
)

replace (
	github.com/decred/dcrd/blockchain => github.com/dnldd/dcrd/blockchain v0.0.0-20190119113654-fad46ce7c9fd
	github.com/decred/dcrd/blockchain/stake => github.com/dnldd/dcrd/blockchain/stake v0.0.0-20190119113654-fad46ce7c9fd
	github.com/decred/dcrd/chaincfg => github.com/dnldd/dcrd/chaincfg v0.0.0-20190119113654-fad46ce7c9fd
	github.com/decred/dcrd/dcrjson => github.com/dnldd/dcrd/dcrjson v0.0.0-20190119113654-fad46ce7c9fd
	github.com/decred/dcrd/dcrutil => github.com/dnldd/dcrd/dcrutil v0.0.0-20190119113654-fad46ce7c9fd
	github.com/decred/dcrd/rpcclient => github.com/dnldd/dcrd/rpcclient v0.0.0-20190119113654-fad46ce7c9fd
)
