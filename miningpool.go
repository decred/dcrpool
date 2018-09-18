package main

// TODO: implement the dcrpool server.

import (
	"context"
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

// NewRPCClient initializes an RPC connection to dcrd for chain notifications.
func NewRPCClient(cfg *config, h *ws.Hub) (*rpcclient.Client, error) {
	ntfnHandlers := &rpcclient.NotificationHandlers{
		OnBlockConnected: func(blkHeader []byte, transactions [][]byte) {
			pLog.Debugf("Block connected: %x", blkHeader)

			if !h.HasConnectedClients() {
				return
			}

			blkHeight, err := ws.FetchBlockHeight(blkHeader)
			if err != nil {
				pLog.Error(err)
				return
			}

			h.Broadcast <- ws.ConnectedBlockNotification(blkHeight)
		},
		OnBlockDisconnected: func(blkHeader []byte) {
			pLog.Debugf("Block disconnected: %x", blkHeader)

			if !h.HasConnectedClients() {
				return
			}

			blkHeight, err := ws.FetchBlockHeight(blkHeader)
			if err != nil {
				pLog.Error(err)
				return
			}

			h.Broadcast <- ws.DisconnectedBlockNotification(blkHeight)
		},
		OnWork: func(blkHeader string, target string) {
			pLog.Debugf("New Work (header: %v , target: %v)", blkHeader,
				target)

			if !h.HasConnectedClients() {
				return
			}

			h.Broadcast <- ws.WorkNotification(blkHeader, target)

		},
		// TODO: possibly need to add new transaction notifications here as well.
	}

	rpcConfig := &rpcclient.ConnConfig{
		Host:         cfg.RPCHost,
		Endpoint:     "ws",
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
		Certificates: cfg.rpccerts,
	}

	client, err := rpcclient.New(rpcConfig, ntfnHandlers)
	if err != nil {
		return nil, err
	}

	if err := client.NotifyWork(); err != nil {
		client.Shutdown()
		return nil, err
	}
	if err := client.NotifyBlocks(); err != nil {
		client.Shutdown()
		return nil, err
	}

	return client, nil
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
	p.hub = ws.NewHub(p.db, p.httpc, p.limiter)
	p.upgrader = websocket.Upgrader{}

	p.rpcclient, err = NewRPCClient(p.cfg, p.hub)
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
	pLog.Info("Shutting down dcrpool...")
	p.hub.Close()
	p.db.Close()
	defer p.cancel()
	if err := p.server.Shutdown(p.ctx); err != nil {
		pLog.Errorf("Shutdown error: %v", err)
	}
}
