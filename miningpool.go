package main

// TODO: implement the dcrpool server.

import (
	"context"
	"net/http"
	"time"

	"github.com/coreos/bbolt"
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
	cfg      *config
	db       *bolt.DB
	server   *http.Server
	httpc    *http.Client
	ctx      context.Context
	cancel   context.CancelFunc
	hub      *ws.Hub
	router   *mux.Router
	limiter  *limiter.RateLimiter
	upgrader websocket.Upgrader
}

// NewMiningPool initializes the mining pool.
func NewMiningPool(config *config) (*MiningPool, error) {
	p := new(MiningPool)
	p.cfg = config

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
	p.hub = ws.NewHub(p.db, p.httpc, p.limiter)
	p.upgrader = websocket.Upgrader{}
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
	pLog.Info("Shutting down dcrpool...")
	p.hub.Close()
	p.db.Close()
	defer p.cancel()
	if err := p.server.Shutdown(p.ctx); err != nil {
		pLog.Errorf("Shutdown error: %v", err)
	}
}
