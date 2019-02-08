// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"runtime"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"

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
}

// NewPool initializes the mining pool.
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
		WalletRPCCertFile: walletRPCCertFile,
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

	return p, nil
}

// shutdown gracefully terminates the mining pool.
func (p *Pool) shutdown() {
	pLog.Info("Shutting down dcrpool.")
	defer p.db.Close()
	defer p.cancel()

	err := p.hub.Persist()
	if err != nil {
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
