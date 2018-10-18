package main

// TODO: implement the dcrpool server.

import (
	"context"
	"math/big"
	"net/http"
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/rpcclient"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"dnldd/dcrpool/database"
	"dnldd/dcrpool/limiter"
	"dnldd/dcrpool/ws"
)

// CORS Rules.
var (
	headersOk = handlers.AllowedHeaders([]string{"X-Requested-With",
		"Content-Type", "Authorization"})
	originsOk = handlers.AllowedOrigins([]string{"*"})
	methodsOk = handlers.AllowedMethods(
		[]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
)

// MiningPool represents a Proof-of-Work Mining pool for Decred.
type MiningPool struct {
	cfg       *config
	db        *bolt.DB
	server    *http.Server
	httpc     *http.Client
	ctx       context.Context
	cancel    context.CancelFunc
	hub       *ws.Hub
	router    *mux.Router
	limiter   *limiter.RateLimiter
	rpcclient *rpcclient.Client
	upgrader  websocket.Upgrader
}

// NewMiningPool initializes the mining pool.
func NewMiningPool(cfg *config) (*MiningPool, error) {
	p := new(MiningPool)
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

	p.limiter = limiter.NewRateLimiter()
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

	hcfg := &ws.HubConfig{
		WalletRPCCertFile: walletRPCCertFile,
		WalletGRPCHost:    cfg.WalletGRPCHost,
		DcrdRPCCfg:        dcrdRPCCfg,
		PoolFee:           new(big.Rat).SetFloat64(cfg.PoolFee),
		MaxGenTime:        new(big.Int).SetUint64(cfg.MaxGenTime),
	}

	p.hub, err = ws.NewHub(p.db, p.httpc, hcfg, p.limiter)
	if err != nil {
		return nil, err
	}

	p.ctx, p.cancel = context.WithTimeout(context.Background(), 5*time.Second)
	return p, nil
}

// listen starts the mining pool server for incoming connections.
func (p *MiningPool) listen() {
	err := p.server.ListenAndServe()
	if err != http.ErrServerClosed {
		pLog.Error(err)
		return
	}
}

// shutdown gracefully terminates the mining pool.
func (p *MiningPool) shutdown() {
	pLog.Info("Shutting down dcrpool.")
	p.hub.Shutdown()
	p.db.Close()
	defer p.cancel()
	if err := p.server.Shutdown(p.ctx); err != nil {
		pLog.Errorf("Shutdown error: %v", err)
	}
}
