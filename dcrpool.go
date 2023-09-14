// Copyright (c) 2019-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrpool/internal/gui"
	"github.com/decred/dcrpool/pool"
)

// signals defines the signals that are handled to do a clean shutdown.
// Conditional compilation is used to also include SIGTERM and SIGHUP on Unix.
var signals = []os.Signal{os.Interrupt}

// newHub returns a new pool hub configured with the provided details that is
// ready to connect to a consensus daemon and wallet in the case of publicly
// available pools.
func newHub(cfg *config, db pool.Database, cancel context.CancelFunc) (*pool.Hub, error) {
	dcrdRPCCfg := rpcclient.ConnConfig{
		Host:         cfg.DcrdRPCHost,
		Endpoint:     "ws",
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
		Certificates: cfg.dcrdRPCCerts,
	}
	iterations := math.Pow(2, float64(256-cfg.net.PowLimit.BitLen()))

	hcfg := &pool.HubConfig{
		DB:                    db,
		NodeRPCConfig:         &dcrdRPCCfg,
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

	return pool.NewHub(cancel, hcfg)
}

// newGUI returns a new GUI configured with the provided details that is ready
// to run.
func newGUI(cfg *config, hub *pool.Hub) (*gui.GUI, error) {
	csrfSecret, err := hub.CSRFSecret()
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
		WithinLimit:           hub.WithinLimit,
		FetchLastWorkHeight:   hub.FetchLastWorkHeight,
		FetchLastPaymentInfo:  hub.FetchLastPaymentInfo,
		FetchMinedWork:        hub.FetchMinedWork,
		FetchWorkQuotas:       hub.FetchWorkQuotas,
		FetchHashData:         hub.FetchHashData,
		AccountExists:         hub.AccountExists,
		FetchArchivedPayments: hub.FetchArchivedPayments,
		FetchPendingPayments:  hub.FetchPendingPayments,
		FetchCacheChannel:     hub.FetchCacheChannel,
	}

	if !cfg.UsePostgres {
		gcfg.HTTPBackupDB = hub.HTTPBackupDB
	}

	return gui.NewGUI(gcfg)
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

	// Primary context that controls the entire process.
	ctx, cancel := context.WithCancel(context.Background())
	defer mpLog.Info("Shutdown complete")

	// Show version and home dir at startup.
	mpLog.Infof("%s version %s (Go version %s %s/%s)", appName,
		Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	mpLog.Infof("Home dir: %s", cfg.HomeDir)

	var db pool.Database
	if cfg.UsePostgres {
		db, err = pool.InitPostgresDB(cfg.PGHost, cfg.PGPort, cfg.PGUser,
			cfg.PGPass, cfg.PGDBName, cfg.PurgeDB)
	} else {
		db, err = pool.InitBoltDB(cfg.DBFile)
	}
	if err != nil {
		cancel()
		mpLog.Errorf("failed to initialize database: %v", err)
		return err
	}
	defer db.Close()

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
				cancel()
			}
		}()
	}

	go func() {
		select {
		case <-ctx.Done():
			return

		case <-interrupt:
			cancel()
		}
	}()

	// Create a hub and GUI instance.
	hub, err := newHub(cfg, db, cancel)
	if err != nil {
		mpLog.Errorf("unable to initialize hub: %v", err)
		return err
	}
	gui, err := newGUI(cfg, hub)
	if err != nil {
		mpLog.Errorf("unable to initialize GUI: %v", err)
		return err
	}

	// Run the GUI in the background.
	go gui.Run(ctx)

	// Run the hub.  This will block until the context is cancelled.
	runHub := func(ctx context.Context, h *pool.Hub) error {
		// Ideally these would go into hub.Run, but the tests don't work
		// properly with this code there due to their tight coupling.
		if err := h.Connect(ctx); err != nil {
			return fmt.Errorf("unable to establish node connections: %w", err)
		}

		if err := h.FetchWork(ctx); err != nil {
			return fmt.Errorf("unable to get work from consensus daemon: %w", err)
		}

		h.Run(ctx)
		return nil
	}
	if err := runHub(ctx, hub); err != nil {
		// Ensure the GUI is signaled to shutdown.
		cancel()
		mpLog.Errorf("unable to run pool hub: %v", err)
		return err
	}

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
