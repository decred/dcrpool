// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/gorilla/mux"

	"github.com/dnldd/dcrpool/database"
	"github.com/dnldd/dcrpool/network"
)

// Pool represents a Proof-of-Work Mining pool for Decred.
type Pool struct {
	cfg     *config
	db      *bolt.DB
	httpc   *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	hub     *network.Hub
	limiter *network.RateLimiter
	server  *http.Server
	router  *mux.Router
}

// initDB handles the creation, upgrading and backup of the database
// (when the pool mode is updated) when needed.
func (p *Pool) initDB() error {
	// Create and open the database.
	db, err := database.OpenDB(p.cfg.DBFile)
	if err != nil {
		return err
	}

	p.db = db
	err = database.CreateBuckets(p.db)
	if err != nil {
		return err
	}

	// Check if the pool mode changed since the last run.
	var switchMode bool
	err = db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return err
		}

		v := pbkt.Get(database.SoloPool)
		if v == nil {
			return nil
		}

		spMode := binary.LittleEndian.Uint32(v) == 1
		if p.cfg.SoloPool != spMode {
			switchMode = true
		}

		return nil
	})

	if err != nil {
		return err
	}

	// If the pool mode changed, backup the current database and purge all data
	// for a clean slate with the updated pool mode.
	if switchMode {
		pLog.Info("Pool mode changed, backing up database before purge.")
		err := database.Backup(p.db)
		if err != nil {
			return err
		}

		err = database.Purge(p.db)
		if err != nil {
			return err
		}
	}

	// If the pool mode did not change, upgrade the database if there is a
	// pending upgrade.
	if !switchMode {
		err = database.Upgrade(p.db)
		if err != nil {
			return err
		}
	}

	return nil
}

// route configures the api routes of the pool.
func (p *Pool) route() {
	p.router = mux.NewRouter()
	p.router.Use(p.limiter.LimiterMiddleware)
	p.router.HandleFunc("/hash", p.hub.FetchHash).Methods("GET")
	p.router.HandleFunc("/connections", p.hub.FetchConnections).Methods("GET")
	p.router.HandleFunc("/mined/{page}", p.hub.FetchMinedWork).Methods("GET")
	p.router.HandleFunc("/work/quotas", p.hub.FetchWorkQuotas).
		Methods("GET")
	p.router.HandleFunc("/work/height", p.hub.FetchLastWorkHeight).
		Methods("GET")
	p.router.HandleFunc("/payment/height", p.hub.FetchLastPaymentHeight).
		Methods("GET")
	p.router.HandleFunc("/account/mined",
		p.hub.FetchMinedWorkByAccount).Methods("POST")
	p.router.HandleFunc("/account/payments",
		p.hub.FetchProcessedPaymentsForAccount).Methods("POST")
}

// serve starts the pool api server.
func (p *Pool) serve() {
	p.route()
	p.server = &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%v", p.cfg.APIPort),
		WriteTimeout: time.Second * 30,
		ReadTimeout:  time.Second * 5,
		IdleTimeout:  time.Second * 30,
		Handler:      p.router,
	}

	pLog.Infof("API server listening on port %v.", p.cfg.APIPort)

	go func() {
		if err := p.server.ListenAndServeTLS(defaultTLSCertFile, defaultTLSKeyFile); err != nil &&
			err != http.ErrServerClosed {
			pLog.Error(err)
		}
	}()
}

// NewPool initializes the mining pool.
func NewPool(cfg *config) (*Pool, error) {
	p := new(Pool)
	p.cfg = cfg

	err := p.initDB()
	if err != nil {
		return nil, err
	}

	p.limiter = network.NewRateLimiter()
	dcrdRPCCfg := &rpcclient.ConnConfig{
		Host:         cfg.DcrdRPCHost,
		Endpoint:     "ws",
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
		Certificates: cfg.dcrdRPCCerts,
	}

	minPmt, err := dcrutil.NewAmount(cfg.MinPayment)
	if err != nil {
		return nil, err
	}

	maxTxFeeReserve, err := dcrutil.NewAmount(cfg.MaxTxFeeReserve)
	if err != nil {
		return nil, err
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	hcfg := &network.HubConfig{
		ActiveNet:         cfg.net,
		WalletRPCCertFile: cfg.WalletRPCCert,
		WalletGRPCHost:    cfg.WalletGRPCHost,
		DcrdRPCCfg:        dcrdRPCCfg,
		PoolFee:           cfg.PoolFee,
		MaxTxFeeReserve:   maxTxFeeReserve,
		MaxGenTime:        new(big.Int).SetUint64(cfg.MaxGenTime),
		PaymentMethod:     cfg.PaymentMethod,
		LastNPeriod:       cfg.LastNPeriod,
		WalletPass:        cfg.WalletPass,
		MinPayment:        minPmt,
		PoolFeeAddrs:      cfg.poolFeeAddrs,
		SoloPool:          cfg.SoloPool,
	}

	p.hub, err = network.NewHub(p.ctx, p.cancel, p.db, p.httpc, hcfg, p.limiter)
	if err != nil {
		return nil, err
	}

	p.serve()

	return p, nil
}

// shutdown gracefully terminates the mining pool.
func (p *Pool) shutdown() {
	pLog.Info("Shutting down dcrpool.")
	defer p.db.Close()
	defer p.cancel()

	ctx, cl := context.WithTimeout(p.ctx, 5*time.Second)
	defer cl()
	if err := p.server.Shutdown(ctx); err != nil {
		pLog.Error(err)
	}
}

func main() {
	// Listen for interrupt signals.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Load configuration and parse command line. This also initializes logging
	// and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		pLog.Error(err)
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

	for {
		select {
		case <-p.ctx.Done():
			p.shutdown()
			return
		case <-interrupt:
			p.cancel()
		}
	}
}
