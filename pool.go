package main

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/rpcclient"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"dnldd/dcrpool/database"
	"dnldd/dcrpool/network"
)

// CORS Rules.
var (
	headersOk = handlers.AllowedHeaders([]string{"X-Requested-With",
		"Content-Type", "Authorization"})
	originsOk = handlers.AllowedOrigins([]string{"*"})
	methodsOk = handlers.AllowedMethods(
		[]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
)

// Pool represents a Proof-of-Work Mining pool for Decred.
type Pool struct {
	cfg       *config
	db        *bolt.DB
	server    *http.Server
	httpc     *http.Client
	ctx       context.Context
	cancel    context.CancelFunc
	hub       *network.Hub
	router    *mux.Router
	limiter   *network.RateLimiter
	rpcclient *rpcclient.Client
	upgrader  websocket.Upgrader
}

// NewPool initializes a mining pool.
func NewPool(cfg *config) (*Pool, error) {
	p := new(Pool)
	p.cfg = cfg

	bolt, err := database.OpenDB(p.cfg.DBFile)
	if err != nil {
		return nil, err
	}
	p.db = bolt
	err = database.CreateBuckets(p.db)
	if err != nil {
		return nil, err
	}

	err = database.Upgrade(p.db)
	if err != nil {
		return nil, err
	}

	p.limiter = network.NewRateLimiter()
	p.router = new(mux.Router)
	p.setupRoutes()
	p.server = &http.Server{
		Addr: p.cfg.Port,
		Handler: handlers.CORS(
			headersOk,
			originsOk,
			methodsOk)(p.limit(p.router)),
	}
	p.upgrader = websocket.Upgrader{}

	dcrdRPCCfg := &rpcclient.ConnConfig{
		Host:         cfg.DcrdRPCHost,
		Endpoint:     "ws",
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
		Certificates: cfg.dcrdRPCCerts,
	}

	hcfg := &network.HubConfig{
		ActiveNet:         cfg.net,
		WalletRPCCertFile: walletRPCCertFile,
		WalletGRPCHost:    cfg.WalletGRPCHost,
		DcrdRPCCfg:        dcrdRPCCfg,
		PoolFee:           new(big.Rat).SetFloat64(cfg.PoolFee),
		MaxGenTime:        new(big.Int).SetUint64(cfg.MaxGenTime),
		PaymentMethod:     cfg.PaymentMethod,
	}

	p.hub, err = network.NewHub(p.db, p.httpc, hcfg, p.limiter)
	if err != nil {
		return nil, err
	}

	p.ctx, p.cancel = context.WithTimeout(context.Background(), 5*time.Second)
	return p, nil
}

// listen starts the mining pool server for incoming connections.
func (p *Pool) listen() {
	err := p.server.ListenAndServe()
	if err != http.ErrServerClosed {
		pLog.Error(err)
		return
	}
}

// shutdown gracefully terminates the mining pool.
func (p *Pool) shutdown() {
	pLog.Info("Shutting down dcrpool.")
	p.hub.Shutdown()
	p.db.Close()
	defer p.cancel()
	if err := p.server.Shutdown(p.ctx); err != nil {
		pLog.Errorf("Shutdown error: %v", err)
	}
}

func main() {
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Listen for interrupt signals.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Load configuration and parse command line. This also initializes logging
	// and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	p, err := NewPool(cfg)
	if err != nil {
		pLog.Error(err)
		return
	}

	pLog.Infof("Version: %s", version())
	pLog.Infof("Runtime: Go version %s", runtime.Version())
	pLog.Infof("Home dir: %s", cfg.HomeDir)
	pLog.Infof("Started dcrpool.")

	go p.listen()
	for {
		select {
		case <-interrupt:
			go p.shutdown()
			return
		}
	}
}
