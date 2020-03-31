// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"

	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/rpcclient/v5"
	"github.com/decred/dcrpool/gui"
	"github.com/decred/dcrpool/pool"
	"github.com/decred/dcrwallet/rpc/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// miningPool represents a decred Proof-of-Work mining pool.
type miningPool struct {
	cfg    *config
	ctx    context.Context
	cancel context.CancelFunc
	hub    *pool.Hub
	gui    *gui.GUI
}

// newPool initializes the mining pool.
func newPool(cfg *config) (*miningPool, error) {
	p := new(miningPool)
	p.cfg = cfg
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
	powLimit := cfg.net.PowLimit
	powLimitF, _ := new(big.Float).SetInt(powLimit).Float64()
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitF)))
	addPort := func(ports map[string]uint32, key string, entry uint32) error {
		var match bool
		var miner string
		for m, port := range ports {
			if port == entry {
				match = true
				miner = m
				break
			}
		}
		if match {
			return fmt.Errorf("%s and %s share port %d", key, miner, entry)
		}
		ports[key] = entry
		return nil
	}

	// Ensure provided miner ports are unique.
	minerPorts := make(map[string]uint32)
	_ = addPort(minerPorts, pool.CPU, cfg.CPUPort)
	err = addPort(minerPorts, pool.InnosiliconD9, cfg.D9Port)
	if err != nil {
		return nil, err
	}
	err = addPort(minerPorts, pool.AntminerDR3, cfg.DR3Port)
	if err != nil {
		return nil, err
	}
	err = addPort(minerPorts, pool.AntminerDR5, cfg.DR5Port)
	if err != nil {
		return nil, err
	}
	err = addPort(minerPorts, pool.WhatsminerD1, cfg.D1Port)
	if err != nil {
		return nil, err
	}
	err = addPort(minerPorts, pool.ObeliskDCR1, cfg.DCR1Port)
	if err != nil {
		return nil, err
	}

	db, err := pool.InitDB(cfg.DBFile, cfg.SoloPool)
	if err != nil {
		return nil, err
	}

	hcfg := &pool.HubConfig{
		DB:                    db,
		ActiveNet:             cfg.net,
		PoolFee:               cfg.PoolFee,
		MaxTxFeeReserve:       maxTxFeeReserve,
		MaxGenTime:            cfg.MaxGenTime,
		PaymentMethod:         cfg.PaymentMethod,
		LastNPeriod:           cfg.LastNPeriod,
		WalletPass:            cfg.WalletPass,
		MinPayment:            minPmt,
		PoolFeeAddrs:          cfg.poolFeeAddrs,
		SoloPool:              cfg.SoloPool,
		NonceIterations:       iterations,
		MinerPorts:            minerPorts,
		MaxConnectionsPerHost: cfg.MaxConnectionsPerHost,
	}
	p.hub, err = pool.NewHub(p.cancel, hcfg)
	if err != nil {
		return nil, err
	}

	// Establish a connection to the mining node.
	ntfnHandlers := p.hub.CreateNotificationHandlers()
	nodeConn, err := rpcclient.New(dcrdRPCCfg, ntfnHandlers)
	if err != nil {
		return nil, err
	}

	if err := nodeConn.NotifyWork(); err != nil {
		nodeConn.Shutdown()
		return nil, err
	}
	if err := nodeConn.NotifyBlocks(); err != nil {
		nodeConn.Shutdown()
		return nil, err
	}

	p.hub.SetNodeConnection(nodeConn)

	// Establish a connection to the wallet if the pool is mining as a
	// publicly available mining pool.
	if !cfg.SoloPool {
		creds, err := credentials.
			NewClientTLSFromFile(cfg.WalletRPCCert, "localhost")
		if err != nil {
			return nil, err
		}

		grpc, err := grpc.Dial(cfg.WalletGRPCHost,
			grpc.WithTransportCredentials(creds))
		if err != nil {
			return nil, err
		}

		walletConn := walletrpc.NewWalletServiceClient(grpc)
		req := &walletrpc.BalanceRequest{RequiredConfirmations: 1}
		_, err = walletConn.Balance(context.TODO(), req)
		if err != nil {
			return nil, err
		}

		p.hub.SetWalletConnection(walletConn, grpc.Close)
	}

	err = p.hub.FetchWork()
	if err != nil {
		return nil, err
	}
	err = p.hub.Listen()
	if err != nil {
		return nil, err
	}

	csrfSecret, err := p.hub.CSRFSecret()
	if err != nil {
		return nil, err
	}

	gcfg := &gui.Config{
		SoloPool:                cfg.SoloPool,
		GUIDir:                  cfg.GUIDir,
		AdminPass:               cfg.AdminPass,
		GUIPort:                 cfg.GUIPort,
		UseLEHTTPS:              cfg.UseLEHTTPS,
		Domain:                  cfg.Domain,
		TLSCertFile:             cfg.TLSCert,
		TLSKeyFile:              cfg.TLSKey,
		ActiveNet:               cfg.net,
		PaymentMethod:           cfg.PaymentMethod,
		Designation:             cfg.Designation,
		PoolFee:                 cfg.PoolFee,
		CSRFSecret:              csrfSecret,
		MinerPorts:              minerPorts,
		WithinLimit:             p.hub.WithinLimit,
		FetchLastWorkHeight:     p.hub.FetchLastWorkHeight,
		FetchLastPaymentHeight:  p.hub.FetchLastPaymentHeight,
		AddPaymentRequest:       p.hub.AddPaymentRequest,
		FetchMinedWork:          p.hub.FetchMinedWork,
		FetchWorkQuotas:         p.hub.FetchWorkQuotas,
		FetchPoolHashRate:       p.hub.FetchPoolHashRate,
		BackupDB:                p.hub.BackupDB,
		FetchClientInfo:         p.hub.FetchClientInfo,
		AccountExists:           p.hub.AccountExists,
		FetchMinedWorkByAccount: p.hub.FetchMinedWorkByAccount,
		FetchPaymentsForAccount: p.hub.FetchPaymentsForAccount,
		FetchAccountClientInfo:  p.hub.FetchAccountClientInfo,
	}
	p.gui, err = gui.NewGUI(gcfg)
	if err != nil {
		p.hub.CloseListeners()
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	p, err := newPool(cfg)
	if err != nil {
		mpLog.Error(err)
		os.Exit(1)
	}

	if cfg.Profile != "" {
		// Start the profiler.
		go func() {
			listenAddr := cfg.Profile
			mpLog.Infof("Creating profiling server listening "+
				"on %s", listenAddr)
			profileRedirect := http.RedirectHandler("/debug/pprof",
				http.StatusSeeOther)
			http.Handle("/", profileRedirect)
			err := http.ListenAndServe(listenAddr, nil)
			if err != nil {
				mpLog.Criticalf(err.Error())
				p.cancel()
			}
		}()
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
	p.gui.Run(p.ctx)
	p.hub.Run(p.ctx)
}
