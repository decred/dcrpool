// Copyright (c) 2019 The Decred developers
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

	"github.com/decred/dcrpool/gui"
	"github.com/decred/dcrpool/pool"
)

// miningPool represents a decred Proof-of-Work mining pool.
type miningPool struct {
	cfg     *config
	db      *bolt.DB
	httpc   *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	hub     *pool.Hub
	gui     *gui.GUI
	limiter *pool.RateLimiter
}

// newPool initializes the mining pool.
func newPool(cfg *config) (*miningPool, error) {
	p := new(miningPool)
	p.cfg = cfg
	p.limiter = pool.NewRateLimiter()
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
	hcfg := &pool.HubConfig{
		ActiveNet:         cfg.net,
		WalletRPCCertFile: cfg.WalletRPCCert,
		WalletGRPCHost:    cfg.WalletGRPCHost,
		DcrdRPCCfg:        dcrdRPCCfg,
		PoolFee:           cfg.PoolFee,
		MaxTxFeeReserve:   maxTxFeeReserve,
		MaxGenTime:        new(big.Int).SetUint64(cfg.MaxGenTime),
		PaymentMethod:     cfg.PaymentMethod,
		DBFIle:            cfg.DBFile,
		LastNPeriod:       cfg.LastNPeriod,
		WalletPass:        cfg.WalletPass,
		MinPayment:        minPmt,
		PoolFeeAddrs:      cfg.poolFeeAddrs,
		SoloPool:          cfg.SoloPool,
	}

	p.hub, err = pool.NewHub(p.ctx, p.cancel, p.httpc, hcfg, p.limiter)
	if err != nil {
		return nil, err
	}

	gcfg := &gui.Config{
		Ctx:           p.ctx,
		SoloPool:      cfg.SoloPool,
		GUIDir:        cfg.GUIDir,
		BackupPass:    cfg.BackupPass,
		GUIPort:       cfg.GUIPort,
		UseLEHTTPS:    cfg.UseLEHTTPS,
		Domain:        cfg.Domain,
		TLSCertFile:   defaultTLSCertFile,
		TLSKeyFile:    defaultTLSKeyFile,
		ActiveNet:     cfg.net,
		PaymentMethod: cfg.PaymentMethod,
		Designation:   cfg.Designation,
		PoolFee:       cfg.PoolFee,
	}

	p.gui, err = gui.NewGUI(gcfg, p.hub, p.db)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func main() {
	// Listen for interrupt signals.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Load configuration and parse command line. This also initializes logging
	// and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		mpLog.Error(err)
		return
	}
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	p, err := newPool(cfg)
	if err != nil {
		mpLog.Error(err)
		return
	}

	mpLog.Infof("Version: %s", version())
	mpLog.Infof("Runtime: Go version %s", runtime.Version())
	mpLog.Infof("Home dir: %s", cfg.HomeDir)
	mpLog.Infof("Started dcrpool.")

	go func() {
		select {
		case <-p.ctx.Done():
			return
		case <-interrupt:
			p.cancel()
		}
	}()

	p.gui.Run()
	p.hub.Run(p.ctx)
}
