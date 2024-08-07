// Copyright (c) 2020-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"github.com/decred/dcrd/chaincfg/v3"
)

type params struct {
	*chaincfg.Params
	dcrdRPCServerPort    string
	walletGRPCServerPort string
	blockExplorerURL     string
}

var mainNetParams = params{
	Params:               chaincfg.MainNetParams(),
	dcrdRPCServerPort:    "9109",
	walletGRPCServerPort: "9111",
	blockExplorerURL:     "https://dcrdata.decred.org",
}

var testNet3Params = params{
	Params:               chaincfg.TestNet3Params(),
	dcrdRPCServerPort:    "19109",
	walletGRPCServerPort: "19111",
	blockExplorerURL:     "https://testnet.dcrdata.org",
}

var simNetParams = params{
	Params:               chaincfg.SimNetParams(),
	dcrdRPCServerPort:    "19556",
	walletGRPCServerPort: "19558",
	blockExplorerURL:     "...",
}
