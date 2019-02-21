// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"sort"
	"strings"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/slog"
	flags "github.com/jessevdk/go-flags"

	"github.com/dnldd/dcrpool/dividend"
	"github.com/dnldd/dcrpool/util"
)

const (
	defaultConfigFilename  = "dcrpool.conf"
	defaultDataDirname     = "data"
	defaultLogLevel        = "debug"
	defaultLogDirname      = "log"
	defaultLogFilename     = "dcrpool.log"
	defaultDBFilename      = "dcrpool.kv"
	defaultRPCCertFilename = "rpc.cert"
	defaultRPCUser         = "dcrp"
	defaultRPCPass         = "dcrppass"
	defaultDcrdRPCHost     = "127.0.0.1:19109"
	defaultWalletGRPCHost  = "127.0.0.1:51028"
	defaultPoolFeeAddr     = ""
	defaultMaxGenTime      = 15
	defaultPoolFee         = 0.01
	defaultLastNPeriod     = 86400 // 1 day
	defaultWalletPass      = ""
	defaultMaxTxFeeReserve = 0.1
	defaultSoloPool        = false
)

var (
	defaultActiveNet     = chaincfg.SimNetParams.Name
	defaultPaymentMethod = dividend.PPS
	defaultMinPayment    = 0.2
	dcrpoolHomeDir       = dcrutil.AppDataDir("dcrpool", false)
	dcrwalletHomeDir     = dcrutil.AppDataDir("dcrwallet", false)
	dcrdHomeDir          = dcrutil.AppDataDir("dcrd", false)
	defaultConfigFile    = filepath.Join(dcrpoolHomeDir, defaultConfigFilename)
	defaultDataDir       = filepath.Join(dcrpoolHomeDir, defaultDataDirname)
	defaultDBFile        = filepath.Join(defaultDataDir, defaultDBFilename)
	dcrdRPCCertFile      = filepath.Join(dcrdHomeDir, defaultRPCCertFilename)
	walletRPCCertFile    = filepath.Join(dcrwalletHomeDir, defaultRPCCertFilename)
	defaultLogDir        = filepath.Join(dcrpoolHomeDir, defaultLogDirname)
)

// runServiceCommand is only set to a real function on Windows.  It is used
// to parse and execute service commands specified via the -s flag.
var runServiceCommand func(string) error

// config defines the configuration options for the pool.
type config struct {
	HomeDir         string   `long:"homedir" description:"Path to application home directory."`
	ConfigFile      string   `long:"configfile" description:"Path to configuration file."`
	DataDir         string   `long:"datadir" description:"The data directory."`
	ActiveNet       string   `long:"activenet" description:"The active network being mined on. {testnet3, mainnet, simnet}"`
	DebugLevel      string   `long:"debuglevel" description:"Logging level for all subsystems. {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems"`
	LogDir          string   `long:"logdir" description:"Directory to log output."`
	DBFile          string   `long:"dbfile" description:"Path to the database file."`
	DcrdRPCHost     string   `long:"dcrdrpchost" description:"The ip:port to establish an RPC connection for dcrd."`
	DcrdRPCCert     string   `long:"dcrdrpccert" description:"The dcrd RPC certificate."`
	WalletGRPCHost  string   `long:"walletgrpchost" description:"The ip:port to establish a GRPC connection for the wallet."`
	WalletRPCCert   string   `long:"walletrpccert" description:"The wallet RPC certificate."`
	RPCUser         string   `long:"rpcuser" description:"Username for RPC connections."`
	RPCPass         string   `long:"rpcpass" default-mask:"-" description:"Password for RPC connections."`
	PoolFeeAddrs    []string `long:"poolfeeaddrs" description:"Payment addresses to use for pool fee transactions. These addresses should be generated from a dedicated wallet account for pool fees."`
	PoolFee         float64  `long:"poolfee" description:"The fee charged for pool participation. eg. 0.01 (1%), 0.05 (5%)."`
	MaxTxFeeReserve float64  `long:"maxtxfeereserve" description:"The maximum amount reserved for transaction fees, in DCR."`
	MaxGenTime      uint64   `long:"maxgentime" description:"The share creation target time for the pool in seconds."`
	PaymentMethod   string   `long:"paymentmethod" description:"The payment method of the pool. {pps, pplns}"`
	LastNPeriod     uint32   `long:"lastnperiod" description:"The period of interest when using the PPLNS payment scheme."`
	WalletPass      string   `long:"walletpass" description:"The wallet passphrase."`
	MinPayment      float64  `long:"minpayment" description:"The minimum payment to process for an account."`
	SoloPool        bool     `long:"solopool" description:"Solo pool mode. This disables payment processing when enabled."`
	poolFeeAddrs    []dcrutil.Address
	dcrdRPCCerts    []byte
	net             *chaincfg.Params
}

// serviceOptions defines the configuration options for the daemon as a service on
// Windows.
type serviceOptions struct {
	ServiceCommand string `short:"s" long:"service" description:"Service command {install, remove, start, stop}"`
}

// validLogLevel returns whether or not logLevel is a valid debug log level.
func validLogLevel(logLevel string) bool {
	_, ok := slog.LevelFromString(logLevel)
	return ok
}

// supportedSubsystems returns a sorted slice of the supported subsystems for
// logging purposes.
func supportedSubsystems() []string {
	// Convert the subsystemLoggers map keys to a slice.
	subsystems := make([]string, 0, len(subsystemLoggers))
	for subsysID := range subsystemLoggers {
		subsystems = append(subsystems, subsysID)
	}

	// Sort the subsystems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) error {
	// When the specified string doesn't have any delimiters, treat it as
	// the log level for all subsystems.
	if !strings.Contains(debugLevel, ",") && !strings.Contains(debugLevel, "=") {
		// Validate debug log level.
		if !validLogLevel(debugLevel) {
			str := "the specified debug level [%v] is invalid"
			return fmt.Errorf(str, debugLevel)
		}

		// Change the logging level for all subsystems.
		setLogLevels(debugLevel)

		return nil
	}

	// Split the specified string into subsystem/level pairs while detecting
	// issues and update the log levels accordingly.
	for _, logLevelPair := range strings.Split(debugLevel, ",") {
		if !strings.Contains(logLevelPair, "=") {
			str := "the specified debug level contains an invalid " +
				"subsystem/level pair [%v]"
			return fmt.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate subsystem.
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "the specified subsystem [%v] is invalid -- " +
				"supported subsytems %v"
			return fmt.Errorf(str, subsysID, supportedSubsystems())
		}

		// Validate log level.
		if !validLogLevel(logLevel) {
			str := "the specified debug level [%v] is invalid"
			return fmt.Errorf(str, logLevel)
		}

		setLogLevel(subsysID, logLevel)
	}

	return nil
}

// fileExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// newConfigParser returns a new command line flags parser.
func newConfigParser(cfg *config, so *serviceOptions, options flags.Options) *flags.Parser {
	parser := flags.NewParser(cfg, options)
	if runtime.GOOS == "windows" {
		parser.AddGroup("Service Options", "Service Options", so)
	}
	return parser
}

// loadConfig initializes and parses the config using a config file and command
// line options.
//
// The configuration proceeds as follows:
// 	1) Start with a default config with sane settings
// 	2) Pre-parse the command line to check for an alternative config file
// 	3) Load configuration file overwriting defaults with any specified options
// 	4) Parse CLI options and overwrite/add any specified options
//
// The above results in dcrpool functioning properly without any config settings
// while still allowing the user to override settings with config files and
// command line options.  Command line options always take precedence.
func loadConfig() (*config, []string, error) {
	// Default config.
	cfg := config{
		HomeDir:         dcrpoolHomeDir,
		ConfigFile:      defaultConfigFile,
		DataDir:         defaultDataDir,
		DBFile:          defaultDBFile,
		DebugLevel:      defaultLogLevel,
		LogDir:          defaultLogDir,
		RPCUser:         defaultRPCUser,
		RPCPass:         defaultRPCPass,
		DcrdRPCHost:     defaultDcrdRPCHost,
		DcrdRPCCert:     dcrdRPCCertFile,
		WalletRPCCert:   walletRPCCertFile,
		WalletGRPCHost:  defaultWalletGRPCHost,
		PoolFeeAddrs:    []string{defaultPoolFeeAddr},
		PoolFee:         defaultPoolFee,
		MaxTxFeeReserve: defaultMaxTxFeeReserve,
		MaxGenTime:      defaultMaxGenTime,
		ActiveNet:       defaultActiveNet,
		PaymentMethod:   defaultPaymentMethod,
		LastNPeriod:     defaultLastNPeriod,
		WalletPass:      defaultWalletPass,
		MinPayment:      defaultMinPayment,
		SoloPool:        defaultSoloPool,
	}

	// Service options which are only added on Windows.
	serviceOpts := serviceOptions{}

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.  Any errors aside from the
	// help message error can be ignored here since they will be caught by
	// the final parse below.
	preCfg := cfg
	preParser := newConfigParser(&preCfg, &serviceOpts, flags.HelpFlag)
	_, err := preParser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		} else if ok && e.Type == flags.ErrHelp {
			fmt.Fprintln(os.Stdout, err)
			os.Exit(0)
		}
	}

	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	usageMessage := fmt.Sprintf("Use %s -h to show usage", appName)

	// Perform service command and exit if specified.  Invalid service
	// commands show an appropriate error.  Only runs on Windows since
	// the runServiceCommand function will be nil when not on Windows.
	if serviceOpts.ServiceCommand != "" && runServiceCommand != nil {
		err := runServiceCommand(serviceOpts.ServiceCommand)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(0)
	}

	// Update the home directory for dcrpool if specified. Since the home
	// directory is updated, other variables need to be updated to
	// reflect the new changes.
	if preCfg.HomeDir != "" {
		cfg.HomeDir, _ = filepath.Abs(preCfg.HomeDir)

		if preCfg.ConfigFile == defaultConfigFile {
			defaultConfigFile = filepath.Join(cfg.HomeDir,
				defaultConfigFilename)
			preCfg.ConfigFile = defaultConfigFile
			cfg.ConfigFile = defaultConfigFile
		} else {
			cfg.ConfigFile = preCfg.ConfigFile
		}
		if preCfg.DataDir == defaultDataDir {
			cfg.DataDir = filepath.Join(cfg.HomeDir, defaultDataDirname)
		} else {
			cfg.DataDir = preCfg.DataDir
		}
		if preCfg.LogDir == defaultLogDir {
			cfg.LogDir = filepath.Join(cfg.HomeDir, defaultLogDirname)
		} else {
			cfg.LogDir = preCfg.LogDir
		}
		if preCfg.DBFile == defaultDBFile {
			cfg.DBFile = filepath.Join(cfg.DataDir, defaultDBFilename)
		} else {
			cfg.DBFile = preCfg.DBFile
		}
	}

	// Create the home directory if it doesn't already exist.
	funcName := "loadConfig"
	err = os.MkdirAll(cfg.HomeDir, 0700)
	if err != nil {
		// Show a nicer error message if it's because a symlink is
		// linked to a directory that does not exist (probably because
		// it's not mounted).
		if e, ok := err.(*os.PathError); ok && os.IsExist(err) {
			if link, lerr := os.Readlink(e.Path); lerr == nil {
				str := "is symlink %s -> %s mounted?"
				err = fmt.Errorf(str, e.Path, link)
			}
		}

		str := "%s: failed to create home directory: %v"
		err := fmt.Errorf(str, funcName, err)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	// Create a default config file when one does not exist and the user did
	// not specify an override.
	if !fileExists(preCfg.ConfigFile) {
		preIni := flags.NewIniParser(preParser)
		err = preIni.WriteFile(preCfg.ConfigFile, flags.IniDefault)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating a default "+
				"config file: %v", err)
		}
	}

	// Load additional config from file.
	var configFileError error
	parser := newConfigParser(&cfg, &serviceOpts, flags.Default)
	if preCfg.ConfigFile != defaultConfigFile {
		err := flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
		if err != nil {
			if _, ok := err.(*os.PathError); !ok {
				fmt.Fprintf(os.Stderr, "Error parsing config "+
					"file: %v\n", err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return nil, nil, err
			}
			configFileError = err
		}
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, usageMessage)
		}
		return nil, nil, err
	}

	cfg.DataDir = util.CleanAndExpandPath(cfg.DataDir)
	cfg.LogDir = util.CleanAndExpandPath(cfg.LogDir)
	logRotator = nil

	// Initialize log rotation.  After log rotation has been initialized, the
	// logger variables may be used.
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename))

	// Create the data directory.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		str := "%s: failed to create data directory: %v"
		err := fmt.Errorf(str, funcName, err)
		return nil, nil, err
	}

	// Special show command to list supported subsystems and exit.
	if cfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err := fmt.Errorf("%s: %v", funcName, err.Error())
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Set the mining active network. Testnet and simnet proof of work
	// parameters are modified here to mirror that of mainnet in order to
	// generate reasonable difficulties for asics.
	switch cfg.ActiveNet {
	case chaincfg.TestNet3Params.Name:
		cfg.net = &chaincfg.TestNet3Params
		cfg.net.PowLimit = chaincfg.MainNetParams.PowLimit
	case chaincfg.MainNetParams.Name:
		cfg.net = &chaincfg.MainNetParams
	case chaincfg.SimNetParams.Name:
		cfg.net = &chaincfg.SimNetParams
		cfg.net.PowLimit = chaincfg.MainNetParams.PowLimit
	default:
		return nil, nil, fmt.Errorf("unknown network provided %v",
			cfg.ActiveNet)
	}

	if !cfg.SoloPool {
		for _, pAddr := range cfg.PoolFeeAddrs {
			addr, err := dcrutil.DecodeAddress(pAddr)
			if err != nil {
				str := "%s: pool fee address '%v' failed to decode: %v"
				err := fmt.Errorf(str, funcName, addr, err)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return nil, nil, err
			}

			// Ensure pool fee address is valid and on the active network.
			if !addr.IsForNet(cfg.net) {
				return nil, nil,
					fmt.Errorf("pool fee address (%v) not on the active network "+
						"(%s)", addr, cfg.ActiveNet)
			}

			cfg.poolFeeAddrs = append(cfg.poolFeeAddrs, addr)
		}
	}

	// Warn about missing config file only after all other configuration is
	// done. This prevents the warning on help messages and invalid
	// options. Note this should go directly before the return.
	if configFileError != nil {
		pLog.Warnf("%v", configFileError)
	}

	// Load Dcrd RPC Certificate.
	if !fileExists(cfg.DcrdRPCCert) {
		return nil, nil,
			fmt.Errorf("dcrd RPC certificate (%v) not found", cfg.DcrdRPCCert)
	}

	cfg.dcrdRPCCerts, err = ioutil.ReadFile(dcrdRPCCertFile)
	if err != nil {
		return nil, nil, err
	}

	if !cfg.SoloPool {
		// Load the wallet RPC certificate.
		if !fileExists(cfg.WalletRPCCert) {
			return nil, nil,
				fmt.Errorf("wallet RPC certificate (%v) not found",
					cfg.WalletRPCCert)
		}
	}

	return &cfg, remainingArgs, nil
}
