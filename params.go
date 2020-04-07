// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"github.com/decred/dcrd/chaincfg/v2"
)

type params struct {
	*chaincfg.Params
	DcrdRPCServerPort    string
	WalletGRPCServerPort string
}

var mainNetParams = params{
	Params:               chaincfg.MainNetParams(),
	DcrdRPCServerPort:    "9109",
	WalletGRPCServerPort: "9111",
}

var testNet3Params = params{
	Params:               chaincfg.TestNet3Params(),
	DcrdRPCServerPort:    "19109",
	WalletGRPCServerPort: "19111",
}

var simNetParams = params{
	Params:               chaincfg.SimNetParams(),
	DcrdRPCServerPort:    "19556",
	WalletGRPCServerPort: "19558",
}
