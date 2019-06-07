// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
)

func main() {

	// Listen for interrupt signals.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Load configuration and parse command line. This also initializes logging
	// and configures it accordingly.
	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Println(err)
		return
	}

	runtime.GOMAXPROCS(cfg.MaxProcs)

	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	log.Infof("Version: %s", version())
	log.Infof("Runtime: Go version %s", runtime.Version())
	log.Infof("Home dir: %s", cfg.HomeDir)
	log.Infof("Started miner.")

	// Initialize and run the client.
	ctx, cancel := context.WithCancel(context.Background())
	miner, err := NewMiner(cfg, cancel)
	if err != nil {
		log.Errorf("Failed to create miner: %v", err)
		return
	}

	go func() {
		select {
		case <-interrupt:
			miner.cancel()

		case <-ctx.Done():
			miner.conn.Close()
			return
		}
	}()

	miner.run(ctx)
}
