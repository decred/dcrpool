// Copyright (c) 2019-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrpool/internal/gui"
	"github.com/decred/dcrpool/pool"
)

// newHub returns a new pool hub configured with the provided details that is
// ready to connect to a consensus daemon and wallet in the case of publicly
// available pools.
func newHub(cfg *config, db pool.Database) (*pool.Hub, error) {
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

	return pool.NewHub(hcfg)
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
		BlockExplorerURL:      cfg.net.blockExplorerURL,
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

	return gui.New(gcfg)
}

// realMain is the real main function for dcrpool.  It is necessary to work
// around the fact that deferred functions do not run when os.Exit() is called.
func realMain() error {
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

	// Get a context whose done channel will be closed when a shutdown signal
	// has been triggered from an OS signal such as SIGINT (Ctrl+C) or when the
	// returned cancel function is manually called.
	//
	// Primary context that controls the entire process.
	ctx, cancel := shutdownListener()
	defer mpLog.Info("Shutdown complete")

	// Show version and home dir at startup.
	mpLog.Infof("%s version %s (Go version %s %s/%s)", appName,
		version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
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
			mpLog.Infof("Creating profiling server listening on %s", listenAddr)
			profileRedirect := http.RedirectHandler("/debug/pprof",
				http.StatusSeeOther)
			http.Handle("/", profileRedirect)
			server := &http.Server{
				Addr:              listenAddr,
				ReadHeaderTimeout: time.Second * 3,
			}
			err := server.ListenAndServe()
			if err != nil {
				mpLog.Critical(err)
				cancel()
			}
		}()
	}

	// Create a hub instance and attempt to perform initial connection and work
	// acquisition.
	hub, err := newHub(cfg, db)
	if err != nil {
		mpLog.Errorf("unable to initialize hub: %v", err)
		return err
	}
	if err := hub.Connect(ctx); err != nil {
		mpLog.Errorf("unable to establish node connections: %v", err)
		return err
	}
	if err := hub.FetchWork(ctx); err != nil {
		mpLog.Errorf("unable to get work from consensus daemon: %v", err)
		return err
	}

	// Create a gui instance.
	gui, err := newGUI(cfg, hub)
	if err != nil {
		mpLog.Errorf("unable to initialize GUI: %v", err)
		return err
	}

	// Run the GUI and hub in the background.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		// Ensure the overall process context is cancelled once Run returns
		// since the pool can't operate without it and it's possible the hub
		// shut itself down due to an error deep in the chain state.
		//
		// The entire pool really generally shouldn't be shutting itself down
		// due to chainstate issues and should instead go into a temporarily
		// disabled state where it stops serving miners while it retries with
		// increasing backoffs while it attempts to resolve whatever caused it
		// to shutdown to begin with.
		//
		// However, since the code is currently structured under the assumption
		// the entire process exits, this retains that behavior.
		hub.Run(ctx)
		cancel()
		wg.Done()
	}()
	go func() {
		gui.Run(ctx)
		wg.Done()
	}()
	wg.Wait()

	// Write a backup of the DB (if not using postgres) once the hub shuts down.
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
