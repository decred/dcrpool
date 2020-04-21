// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/elliptic"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	flags "github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh/terminal"
	te "golang.org/x/crypto/ssh/terminal"

	"github.com/decred/dcrd/certgen"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrpool/pool"
	"github.com/decred/slog"
)

const (
	defaultConfigFilename        = "dcrpool.conf"
	defaultDataDirname           = "data"
	defaultLogLevel              = "debug"
	defaultLogDirname            = "log"
	defaultLogFilename           = "dcrpool.log"
	defaultDBFilename            = "dcrpool.kv"
	defaultTLSCertFilename       = "dcrpool.cert"
	defaultTLSKeyFilename        = "dcrpool.key"
	defaultDcrdRPCHost           = "127.0.0.1"
	defaultWalletGRPCHost        = "127.0.0.1"
	defaultMaxGenTime            = time.Second * 15
	defaultPoolFee               = 0.01
	defaultLastNPeriod           = time.Hour * 24
	defaultMaxTxFeeReserve       = 0.1
	defaultSoloPool              = false
	defaultGUIPort               = 8080
	defaultGUIDir                = "gui"
	defaultUseLEHTTPS            = false
	defaultCPUPort               = 5550
	defaultDCR1Port              = 5551
	defaultD9Port                = 5552
	defaultDR3Port               = 5553
	defaultDR5Port               = 5554
	defaultD1Port                = 5555
	defaultDesignation           = "YourPoolNameHere"
	defaultMaxConnectionsPerHost = 100 // 100 connected clients per host
)

var (
	defaultActiveNet     = chaincfg.SimNetParams().Name
	defaultPaymentMethod = pool.PPLNS
	defaultMinPayment    = 0.2
	dcrpoolHomeDir       = dcrutil.AppDataDir("dcrpool", false)
	defaultConfigFile    = filepath.Join(dcrpoolHomeDir, defaultConfigFilename)
	defaultDataDir       = filepath.Join(dcrpoolHomeDir, defaultDataDirname)
	defaultDBFile        = filepath.Join(defaultDataDir, defaultDBFilename)
	defaultLogDir        = filepath.Join(dcrpoolHomeDir, defaultLogDirname)
	defaultTLSCertFile   = filepath.Join(dcrpoolHomeDir, defaultTLSCertFilename)
	defaultTLSKeyFile    = filepath.Join(dcrpoolHomeDir, defaultTLSKeyFilename)
)

// runServiceCommand is only set to a real function on Windows.  It is used
// to parse and execute service commands specified via the -s flag.
var runServiceCommand func(string) error

// config defines the configuration options for the pool.
type config struct {
	HomeDir               string        `long:"homedir" ini-name:"homedir" description:"Path to application home directory."`
	ConfigFile            string        `long:"configfile" ini-name:"configfile" description:"Path to configuration file."`
	DataDir               string        `long:"datadir" ini-name:"datadir" description:"The data directory."`
	ActiveNet             string        `long:"activenet" ini-name:"activenet" description:"The active network being mined on. {testnet3, mainnet, simnet}"`
	GUIPort               uint32        `long:"guiport" ini-name:"guiport" description:"The pool GUI port."`
	DebugLevel            string        `long:"debuglevel" ini-name:"debuglevel" description:"Logging level for all subsystems. {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems"`
	LogDir                string        `long:"logdir" ini-name:"logdir" description:"Directory to log output."`
	DBFile                string        `long:"dbfile" ini-name:"dbfile" description:"Path to the database file."`
	DcrdRPCHost           string        `long:"dcrdrpchost" ini-name:"dcrdrpchost" description:"The ip:port to establish an RPC connection for dcrd."`
	DcrdRPCCert           string        `long:"dcrdrpccert" ini-name:"dcrdrpccert" description:"The dcrd RPC certificate."`
	WalletGRPCHost        string        `long:"walletgrpchost" ini-name:"walletgrpchost" description:"The ip:port to establish a GRPC connection for the wallet."`
	WalletRPCCert         string        `long:"walletrpccert" ini-name:"walletrpccert" description:"The wallet RPC certificate."`
	RPCUser               string        `long:"rpcuser" ini-name:"rpcuser" description:"Username for RPC connections."`
	RPCPass               string        `long:"rpcpass" ini-name:"rpcpass" default-mask:"-" description:"Password for RPC connections."`
	PoolFeeAddrs          []string      `long:"poolfeeaddrs" ini-name:"poolfeeaddrs" description:"Payment addresses to use for pool fee transactions. These addresses should be generated from a dedicated wallet account for pool fees."`
	PoolFee               float64       `long:"poolfee" ini-name:"poolfee" description:"The fee charged for pool participation. eg. 0.01 (1%), 0.05 (5%)."`
	MaxTxFeeReserve       float64       `long:"maxtxfeereserve" ini-name:"maxtxfeereserve" description:"The maximum amount reserved for transaction fees, in DCR."`
	MaxGenTime            time.Duration `long:"maxgentime" ini-name:"maxgentime" description:"The share creation target time for the pool. Valid time units are {s,m,h}. Minimum 2 seconds. This currently should be below 30 seconds to increase the likelihood a work submission for clients between new work distributions by the pool."`
	PaymentMethod         string        `long:"paymentmethod" ini-name:"paymentmethod" description:"The payment method of the pool. {pps, pplns}"`
	LastNPeriod           time.Duration `long:"lastnperiod" ini-name:"lastnperiod" description:"The time period of interest when using PPLNS payment scheme. Valid time units are {s,m,h}. Minimum 60 seconds."`
	WalletPass            string        `long:"walletpass" ini-name:"walletpass" description:"The wallet passphrase."`
	MinPayment            float64       `long:"minpayment" ini-name:"minpayment" description:"The minimum payment to process for an account."`
	SoloPool              bool          `long:"solopool" ini-name:"solopool" description:"Solo pool mode. This disables payment processing when enabled."`
	AdminPass             string        `long:"adminpass" ini-name:"adminpass" description:"The admin password."`
	GUIDir                string        `long:"guidir" ini-name:"guidir" description:"The path to the directory containing the pool's user interface assets (templates, css etc.)"`
	Domain                string        `long:"domain" ini-name:"domain" description:"The domain of the mining pool, required for TLS."`
	UseLEHTTPS            bool          `long:"uselehttps" ini-name:"uselehttps" description:"This enables HTTPS using a Letsencrypt certificate. By default the pool uses a self-signed certificate for HTTPS."`
	TLSCert               string        `long:"tlscert" ini-name:"tlscert" description:"Path to the TLS cert file."`
	TLSKey                string        `long:"tlskey" ini-name:"tlskey" description:"Path to the TLS key file."`
	Designation           string        `long:"designation" ini-name:"designation" description:"The designated codename for this pool. Customises the logo in the top toolbar."`
	MaxConnectionsPerHost uint32        `long:"maxconnperhost" ini-name:"maxconnperhost" description:"The maximum number of connections allowed per host."`
	Profile               string        `long:"profile" ini-name:"profile" description:"Enable HTTP profiling on given [addr:]port -- NOTE port must be between 1024 and 65536"`
	CPUPort               uint32        `long:"cpuport" ini-name:"cpuport" description:"CPU miner connection port."`
	D9Port                uint32        `long:"d9port" ini-name:"d9port" description:"Innosilicon D9 connection port."`
	DR3Port               uint32        `long:"dr3port" ini-name:"dr3port" description:"Antminer DR3 connection port."`
	DR5Port               uint32        `long:"dr5port" ini-name:"dr5port" description:"Antminer DR5 connection port."`
	D1Port                uint32        `long:"d1port" ini-name:"d1port" description:"Whatsminer D1 connection port."`
	DCR1Port              uint32        `long:"dcr1port" ini-name:"dcr1port" description:"Obelisk DCR1 connection port."`
	poolFeeAddrs          []dcrutil.Address
	dcrdRPCCerts          []byte
	net                   *params
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
	if _, err := os.Stat(name); os.IsNotExist(err) {
		return false
	}
	return true
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string) error {
	org := "dcrpool autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := certgen.NewTLSCertPair(elliptic.P256(), org,
		validUntil, nil)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = ioutil.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = ioutil.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	return nil
}

// newConfigParser returns a new command line flags parser.
func newConfigParser(cfg *config, so *serviceOptions, options flags.Options) (*flags.Parser, error) {
	parser := flags.NewParser(cfg, options)
	if runtime.GOOS == "windows" {
		_, err := parser.AddGroup("Service Options", "Service Options", so)
		if err != nil {
			return nil, err
		}
	}
	return parser, nil
}

// cleanAndExpandPath expands environment variables and leading ~ in the
// passed path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Nothing to do when no path is given.
	if path == "" {
		return path
	}

	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but the variables can still be expanded via POSIX-style
	// $VARIABLE.
	path = os.ExpandEnv(path)

	if !strings.HasPrefix(path, "~") {
		return filepath.Clean(path)
	}

	// Expand initial ~ to the current user's home directory, or ~otheruser
	// to otheruser's home directory.  On Windows, both forward and backward
	// slashes can be used.
	path = path[1:]

	var pathSeparators string
	if runtime.GOOS == "windows" {
		pathSeparators = string(os.PathSeparator) + "/"
	} else {
		pathSeparators = string(os.PathSeparator)
	}

	userName := ""
	if i := strings.IndexAny(path, pathSeparators); i != -1 {
		userName = path[:i]
		path = path[i:]
	}

	homeDir := ""
	var u *user.User
	var err error
	if userName == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(userName)
	}
	if err == nil {
		homeDir = u.HomeDir
	}
	// Fallback to CWD if user lookup fails or user has no home directory.
	if homeDir == "" {
		homeDir = "."
	}

	return filepath.Join(homeDir, path)
}

// normalizeAddress returns addr with the passed default port appended if
// there is not already a port specified.
func normalizeAddress(addr, defaultPort string) string {
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return net.JoinHostPort(addr, defaultPort)
	}
	return addr
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
func loadConfig(ctx context.Context) (*config, []string, error) {
	// Default config.
	cfg := config{
		HomeDir:               dcrpoolHomeDir,
		ConfigFile:            defaultConfigFile,
		DataDir:               defaultDataDir,
		DBFile:                defaultDBFile,
		DebugLevel:            defaultLogLevel,
		LogDir:                defaultLogDir,
		DcrdRPCHost:           defaultDcrdRPCHost,
		WalletGRPCHost:        defaultWalletGRPCHost,
		PoolFee:               defaultPoolFee,
		MaxTxFeeReserve:       defaultMaxTxFeeReserve,
		MaxGenTime:            defaultMaxGenTime,
		ActiveNet:             defaultActiveNet,
		PaymentMethod:         defaultPaymentMethod,
		LastNPeriod:           defaultLastNPeriod,
		MinPayment:            defaultMinPayment,
		SoloPool:              defaultSoloPool,
		GUIPort:               defaultGUIPort,
		GUIDir:                defaultGUIDir,
		UseLEHTTPS:            defaultUseLEHTTPS,
		TLSCert:               defaultTLSCertFile,
		TLSKey:                defaultTLSKeyFile,
		Designation:           defaultDesignation,
		MaxConnectionsPerHost: defaultMaxConnectionsPerHost,
		CPUPort:               defaultCPUPort,
		D9Port:                defaultD9Port,
		DR3Port:               defaultDR3Port,
		DR5Port:               defaultDR5Port,
		D1Port:                defaultD1Port,
		DCR1Port:              defaultDCR1Port,
	}

	// Service options which are only added on Windows.
	serviceOpts := serviceOptions{}

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.  Any errors aside from the
	// help message error can be ignored here since they will be caught by
	// the final parse below.
	preCfg := cfg
	preParser, err := newConfigParser(&preCfg, &serviceOpts, flags.HelpFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = preParser.Parse()
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
		if preCfg.TLSCert == defaultTLSCertFile {
			cfg.TLSCert = filepath.Join(cfg.HomeDir, defaultTLSCertFilename)
		} else {
			cfg.TLSCert = preCfg.TLSCert
		}

		if preCfg.TLSKey == defaultTLSKeyFile {
			cfg.TLSKey = filepath.Join(cfg.HomeDir, defaultTLSKeyFilename)
		} else {
			cfg.TLSKey = preCfg.TLSKey
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
		err = preIni.WriteFile(preCfg.ConfigFile,
			flags.IniIncludeComments|flags.IniIncludeDefaults)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating a default "+
				"config file: %v", err)
		}
	}

	// Load additional config from file.
	var configFileError error
	parser, err := newConfigParser(&cfg, &serviceOpts, flags.Default)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			fmt.Fprintf(os.Stderr, "error parsing config file: %v\n", err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}
		configFileError = err
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, usageMessage)
		}
		return nil, nil, err
	}

	cfg.DataDir = cleanAndExpandPath(cfg.DataDir)
	cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	logRotator = nil

	// Initialize log rotation.  After log rotation has been initialized, the
	// logger variables may be used.
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename))

	// Ensure the admin password is set, if not prompt the user.
	if cfg.AdminPass == "" {
	Loop:
		for {
			pass, err := adminPassPrompt(ctx, "Enter the admin pass for your mining pool", true)
			cfg.AdminPass = string(pass)
			if err != nil {
				// prompt for adminpass again if error occurs
				continue Loop
			}

			// exit the while loop
			break
		}
	}

	// Ensure the dcrd rpc username is set.
	if cfg.RPCUser == "" {
		str := "%s: the rpcuser option is not set"
		err := fmt.Errorf(str, funcName)
		return nil, nil, err
	}

	// Ensure the dcrd rpc password is set.
	if cfg.RPCPass == "" {
		str := "%s: the rpcpass option is not set"
		err := fmt.Errorf(str, funcName)
		return nil, nil, err
	}

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

	// Set the mining active network.
	switch cfg.ActiveNet {
	case chaincfg.TestNet3Params().Name:
		cfg.net = &testNet3Params
	case chaincfg.MainNetParams().Name:
		cfg.net = &mainNetParams
	case chaincfg.SimNetParams().Name:
		cfg.net = &simNetParams
	default:
		return nil, nil, fmt.Errorf("unknown network provided %v",
			cfg.ActiveNet)
	}

	// Add default ports for the active network if there are no ports specified.
	cfg.DcrdRPCHost = normalizeAddress(cfg.DcrdRPCHost, cfg.net.DcrdRPCServerPort)
	cfg.WalletGRPCHost = normalizeAddress(cfg.WalletGRPCHost, cfg.net.WalletGRPCServerPort)

	if !cfg.SoloPool {
		// Ensure a valid payment method is set.
		if cfg.PaymentMethod != pool.PPS && cfg.PaymentMethod != pool.PPLNS {
			str := "%s: paymentmethod must be either %s or %s"
			err := fmt.Errorf(str, funcName, pool.PPS, pool.PPLNS)
			return nil, nil, err
		}

		// Ensure pool fee is valid.
		if cfg.PoolFee < 0 || cfg.PoolFee > 1 {
			str := "%s: poolfee should be between 0 and 1"
			err := fmt.Errorf(str, funcName)
			return nil, nil, err
		}

		// Ensure the passphrase to unlock the wallet is provided.
		// Wallet passphrase is required to pay dividends to pool contributors.
		if cfg.WalletPass == "" {
			str := "%s: the walletpass option is not set"
			err := fmt.Errorf(str, funcName)
			return nil, nil, err
		}

		// Ensure address to collect pool fees is provided.
		// jessevdk/go-flags does not automatically split the string, so at this
		// point either the array is empty, or the first item of the array
		// contains the full string.
		if len(cfg.PoolFeeAddrs) == 0 || len(cfg.PoolFeeAddrs[0]) == 0 {
			str := "%s: the poolfeeaddrs option is not set"
			err := fmt.Errorf(str, funcName)
			return nil, nil, err
		}

		// Split the string into an array, and parse pool fee addresses.
		cfg.PoolFeeAddrs = strings.Split(cfg.PoolFeeAddrs[0], ",")
		for _, pAddr := range cfg.PoolFeeAddrs {
			addr, err := dcrutil.DecodeAddress(pAddr, cfg.net)
			if err != nil {
				str := "%s: pool fee address '%v' failed to decode: %v"
				err := fmt.Errorf(str, funcName, pAddr, err)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return nil, nil, err
			}

			cfg.poolFeeAddrs = append(cfg.poolFeeAddrs, addr)
		}
	}

	// Do not allow maxgentime durations that are too short.
	if cfg.MaxGenTime < time.Second*2 {
		str := "%s: the maxgentime option may not be less " +
			"than 2s -- parsed [%v]"
		err := fmt.Errorf(str, funcName, cfg.MaxGenTime)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Do not allow lastnperiod durations that are too short.
	if cfg.LastNPeriod < time.Second*60 {
		str := "%s: the lastnperiod option may not be less " +
			"than 60s -- parsed [%v]"
		err := fmt.Errorf(str, funcName, cfg.LastNPeriod)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Warn about missing config file only after all other configuration is
	// done. This prevents the warning on help messages and invalid
	// options. Note this should go directly before the return.
	if configFileError != nil {
		mpLog.Warnf("%v", configFileError)
	}

	// Ensure a domain is set if HTTPS via letsencrypt is preferred.
	if cfg.UseLEHTTPS && cfg.Domain == "" {
		return nil, nil, fmt.Errorf("a valid domain is required for HTTPS " +
			"via letsencrypt")
	}

	// Generate self-signed TLS cert and key if they do not already exist.
	if !cfg.UseLEHTTPS && (!fileExists(cfg.TLSCert) || !fileExists(cfg.TLSKey)) {
		err := genCertPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, nil,
				fmt.Errorf("failed to generate dcrpool's TLS cert/key: %v", err)
		}
	}

	// Load dcrd RPC certificate.
	if !fileExists(cfg.DcrdRPCCert) {
		return nil, nil,
			fmt.Errorf("dcrd RPC certificate (%v) not found", cfg.DcrdRPCCert)
	}

	cfg.dcrdRPCCerts, err = ioutil.ReadFile(cfg.DcrdRPCCert)
	if err != nil {
		return nil, nil, err
	}

	// Validate format of profile, can be an address:port, or just a port.
	if cfg.Profile != "" {
		// If profile is just a number, then add a default host of "127.0.0.1"
		// such that Profile is a valid tcp address.
		if _, err := strconv.Atoi(cfg.Profile); err == nil {
			cfg.Profile = net.JoinHostPort("127.0.0.1", cfg.Profile)
		}

		// Ensure the profiling address is a valid tcp address.
		_, portStr, err := net.SplitHostPort(cfg.Profile)
		if err != nil {
			str := "%s: profile: %s"
			err := fmt.Errorf(str, funcName, err)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		// Finally, check the port is in range.
		if port, _ := strconv.Atoi(portStr); port < 1024 || port > 65535 {
			str := "%s: profile: address %s: port must be between 1024 and 65535"
			err := fmt.Errorf(str, funcName, cfg.Profile)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}
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

func adminPassPrompt(ctx context.Context, prefix string, confirm bool) (passphrase []byte, err error) {
	os.Stdout.Sync()
	c := make(chan struct{}, 1)
	go func() {
		passphrase, err = passPrompt(bufio.NewReader(os.Stdin), prefix, confirm)
		c <- struct{}{}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c:
		return passphrase, err
	}
}

// PassPrompt prompts the user for a passphrase with the given prefix.  The
// function will ask the user to confirm the passphrase and will repeat the
// prompts until they enter a matching response.
func passPrompt(reader *bufio.Reader, prefix string, confirm bool) ([]byte, error) {
	// Prompt the user until they enter a passphrase.
	prompt := fmt.Sprintf("%s: ", prefix)
	for {
		fmt.Print(prompt)
		var pass []byte
		var err error
		fd := int(os.Stdin.Fd())
		if terminal.IsTerminal(fd) {
			pass, err = terminal.ReadPassword(fd)
		} else {
			pass, err = reader.ReadBytes('\n')
			if errors.Is(err, io.EOF) {
				err = nil
			}
		}
		if err != nil {
			return nil, err
		}
		fmt.Print("\n")
		pass = bytes.TrimSpace(pass)
		if len(pass) == 0 {
			continue
		}

		if !confirm {
			return pass, nil
		}

		fmt.Print("Confirm passphrase: ")
		confirm, err := te.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return nil, err
		}
		fmt.Print("\n")
		confirm = bytes.TrimSpace(confirm)
		if !bytes.Equal(pass, confirm) {
			fmt.Println("The entered passphrases do not match")
			continue
		}

		return pass, nil
	}
}
