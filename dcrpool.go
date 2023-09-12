// Copyright (c) 2019-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrpool/gui"
	"github.com/decred/dcrpool/pool"
)

// signals defines the signals that are handled to do a clean shutdown.
// Conditional compilation is used to also include SIGTERM and SIGHUP on Unix.
var signals = []os.Signal{os.Interrupt}

// miningPool represents a decred proof-of-Work mining pool.
type miningPool struct {
	ctx    context.Context
	cancel context.CancelFunc
	hub    *pool.Hub
	gui    *gui.GUI
}

// newPool initializes the mining pool.
func newPool(db pool.Database, cfg *config) (*miningPool, error) {
	p := new(miningPool)
	dcrdRPCCfg := &rpcclient.ConnConfig{
		Host:         cfg.DcrdRPCHost,
		Endpoint:     "ws",
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
		Certificates: cfg.dcrdRPCCerts,
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	powLimit := cfg.net.PowLimit
	powLimitF, _ := new(big.Float).SetInt(powLimit).Float64()
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitF)))

	hcfg := &pool.HubConfig{
		DB:                    db,
		NodeRPCConfig:         dcrdRPCCfg,
		WalletRPCCert:         cfg.WalletRPCCert,
		WalletTLSCert:         cfg.WalletTLSCert,
		WalletTLSKey:          cfg.WalletTLSKey,
		WalletGRPCHost:        cfg.WalletGRPCHost,
		ActiveNet:             cfg.net.Params,
		PoolFee:               cfg.PoolFee,
		MaxGenTime:            cfg.MaxGenTime,
		PaymentMethod:         cfg.PaymentMethod,
		LastNPeriod:           cfg.LastNPeriod,
		WalletPass:            cfg.WalletPass,
		PoolFeeAddrs:          cfg.poolFeeAddrs,
		SoloPool:              cfg.SoloPool,
		NonceIterations:       iterations,
		MinerListen:           cfg.MinerListen,
		MaxConnectionsPerHost: cfg.MaxConnectionsPerHost,
		WalletAccount:         cfg.WalletAccount,
		CoinbaseConfTimeout:   cfg.CoinbaseConfTimeout,
		MonitorCycle:          cfg.MonitorCycle,
		MaxUpgradeTries:       cfg.MaxUpgradeTries,
		ClientTimeout:         cfg.clientTimeout,
	}

	var err error
	p.hub, err = pool.NewHub(p.cancel, hcfg)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize hub: %w", err)
	}

	err = p.hub.Connect(p.ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to establish node connections: %w", err)
	}

	err = p.hub.FetchWork(p.ctx)
	if err != nil {
		return nil, err
	}

	csrfSecret, err := p.hub.CSRFSecret()
	if err != nil {
		return nil, err
	}

	gcfg := &gui.Config{
		SoloPool:              cfg.SoloPool,
		GUIDir:                cfg.GUIDir,
		AdminPass:             cfg.AdminPass,
		GUIListen:             cfg.GUIListen,
		UseLEHTTPS:            cfg.UseLEHTTPS,
		NoGUITLS:              cfg.NoGUITLS,
		Domain:                cfg.Domain,
		TLSCertFile:           cfg.GUITLSCert,
		TLSKeyFile:            cfg.GUITLSKey,
		ActiveNetName:         cfg.net.Params.Name,
		BlockExplorerURL:      cfg.net.BlockExplorerURL,
		PaymentMethod:         cfg.PaymentMethod,
		Designation:           cfg.Designation,
		PoolFee:               cfg.PoolFee,
		CSRFSecret:            csrfSecret,
		MinerListen:           cfg.MinerListen,
		WithinLimit:           p.hub.WithinLimit,
		FetchLastWorkHeight:   p.hub.FetchLastWorkHeight,
		FetchLastPaymentInfo:  p.hub.FetchLastPaymentInfo,
		FetchMinedWork:        p.hub.FetchMinedWork,
		FetchWorkQuotas:       p.hub.FetchWorkQuotas,
		FetchHashData:         p.hub.FetchHashData,
		AccountExists:         p.hub.AccountExists,
		FetchArchivedPayments: p.hub.FetchArchivedPayments,
		FetchPendingPayments:  p.hub.FetchPendingPayments,
		FetchCacheChannel:     p.hub.FetchCacheChannel,
	}

	if !cfg.UsePostgres {
		gcfg.HTTPBackupDB = p.hub.HTTPBackupDB
	}

	p.gui, err = gui.NewGUI(gcfg)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// realMain is the real main function for dcrpool.  It is necessary to work
// around the fact that deferred functions do not run when os.Exit() is called.
func realMain() error {
	// Listen for interrupt signals.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, signals...)

	// Load configuration and parse command line. This also initializes
	// logging and configures it accordingly.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	cfg, _, err := loadConfig(appName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		var e suppressUsageError
		if !errors.As(err, &e) {
			usageMessage := fmt.Sprintf("Use %s -h to show usage", appName)
			fmt.Fprintln(os.Stderr, usageMessage)
		}
		return err
	}
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()
	defer mpLog.Info("Shutdown complete")

	var db pool.Database
	if cfg.UsePostgres {
		db, err = pool.InitPostgresDB(cfg.PGHost, cfg.PGPort, cfg.PGUser,
			cfg.PGPass, cfg.PGDBName, cfg.PurgeDB)
	} else {
		db, err = pool.InitBoltDB(cfg.DBFile)
	}
	if err != nil {
		mpLog.Errorf("failed to initialize database: %v", err)
		return err
	}
	defer db.Close()

	p, err := newPool(db, cfg)
	if err != nil {
		mpLog.Errorf("failed to initialize pool: %v", err)
		return err
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

	mpLog.Infof("%s version %s (Go version %s %s/%s)", appName,
		Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	mpLog.Infof("Home dir: %s", cfg.HomeDir)
	mpLog.Infof("Started dcrpool")

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

	// hub.Run() blocks until the pool is fully shut down. When it returns,
	// write a backup of the DB (if not using postgres), and then close the DB.
	if !cfg.UsePostgres {
		mpLog.Info("Backing up database.")
		err = db.Backup(pool.BoltBackupFile)
		if err != nil {
			mpLog.Errorf("Failed to write database backup file: %v", err)
		}
	}

	mpLog.Info("Hub shutdown complete")
	return nil
}

func main() {
	// Work around defer not working after os.Exit()
	if err := realMain(); err != nil {
		os.Exit(1)
	}
}
