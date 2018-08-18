package main

// TODO: implement the dcrpool server.

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coreos/bbolt"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"dnldd/dcrpool/database"
	"dnldd/dcrpool/mgmt"
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
	hub      *ws.Hub
	router   *mux.Router
	sessions map[string]*mgmt.Session // k/v -> accountID/Sesssion
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

	p.router = new(mux.Router)
	p.setupRoutes()
	p.server = &http.Server{
		Addr: p.cfg.Port,
		Handler: handlers.CORS(
			headersOk,
			originsOk,
			methodsOk)(p.router),
	}
	p.hub = ws.NewHub(p.db, p.httpc)
	p.upgrader = websocket.Upgrader{}
	p.sessions = make(map[string]*mgmt.Session)

	return p, nil
}

// Shutdown gracefully terminates the server when shutdown is signalled.
func (p *MiningPool) shutdown(ctx context.Context) context.Context {
	ctx, done := context.WithCancel(ctx)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		defer done()
		<-quit
		signal.Stop(quit)
		close(quit)
		p.hub.Close()
		p.db.Close()
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := p.server.Shutdown(ctx); err != nil {
			mpLog.Errorf("failed at gracefully shuting down server: %v", err)
		}
	}()

	return ctx
}
