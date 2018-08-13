package main

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
)

var cfg *config
var pool *MiningPool

func main() {
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Load configuration and parse command line. This also initializes logging
	// and configures it accordingly.
	tcfg, _, err := loadConfig()
	if err != nil {
		fmt.Println(err)
		return
	}
	cfg = tcfg
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	// Show version and home dir at startup.
	mpLog.Infof("Version %s (Go version %s)", version(), runtime.Version())
	mpLog.Infof("Home dir: %s", cfg.HomeDir)

	pool, err := NewMiningPool(cfg)
	if err != nil {
		mpLog.Error(err)
		return
	}

	context := pool.shutdown(context.Background())
	err = pool.server.ListenAndServeTLS(pool.cfg.TLSCert, pool.cfg.TLSKey)
	if err != http.ErrServerClosed {
		mpLog.Error(err)
		return
	}

	// Signal shutdown.
	<-context.Done()
}
