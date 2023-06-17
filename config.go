// Copyright (c) 2019-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"crypto/elliptic"
	"errors"
	"fmt"
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

	"github.com/decred/dcrd/certgen"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrpool/pool"
	"github.com/decred/slog"
)

const (
	defaultConfigFilename        = "dcrpool.conf"
	defaultDataDirname           = "data"
	defaultLogLevel              = "info"
	defaultLogDirname            = "log"
	defaultLogFilename           = "dcrpool.log"
	defaultDBFilename            = "dcrpool.kv"
	defaultGUITLSCertFilename    = "dcrpool.cert"
	defaultGUITLSKeyFilename     = "dcrpool.key"
	defaultWalletTLSCertFilename = "wallet.cert"
	defaultWalletTLSKeyFilename  = "wallet.key"
	defaultDcrdRPCHost           = "127.0.0.1"
	defaultWalletGRPCHost        = "127.0.0.1"
	defaultMaxGenTime            = time.Second * 15
	defaultPoolFee               = 0.01
	defaultLastNPeriod           = time.Hour * 24
	defaultSoloPool              = false
	defaultGUIPort               = "8080"
	defaultGUIListen             = "0.0.0.0"
	defaultGUIDir                = "gui"
	defaultUseLEHTTPS            = false
	defaultMinerPort             = "5550"
	defaultMinerListen           = "0.0.0.0"
	defaultDesignation           = "YourPoolNameHere"
	defaultMaxConnectionsPerHost = 100 // 100 connected clients per host
	defaultWalletAccount         = 0
	defaultCoinbaseConfTimeout   = time.Minute * 5 // one block time
	defaultUsePostgres           = false
	defaultPGHost                = "127.0.0.1"
	defaultPGPort                = 5432
	defaultPGUser                = "dcrpooluser"
	defaultPGPass                = "12345"
	defaultPGDBName              = "dcrpooldb"
	defaultMonitorCycle          = time.Minute * 2
	defaultMaxUpgradeTries       = 10
	defaultNoGUITLS              = false
)

var (
	defaultActiveNet     = chaincfg.SimNetParams().Name
	defaultPaymentMethod = pool.PPLNS
	dcrpoolHomeDir       = dcrutil.AppDataDir("dcrpool", false)
	defaultConfigFile    = filepath.Join(dcrpoolHomeDir, defaultConfigFilename)
	defaultDataDir       = filepath.Join(dcrpoolHomeDir, defaultDataDirname)
	defaultDBFile        = filepath.Join(defaultDataDir, defaultDBFilename)
	defaultLogDir        = filepath.Join(dcrpoolHomeDir, defaultLogDirname)

	// This keypair is solely for enabling HTTPS connections to the pool's
	// web interface.
	defaultGUITLSCertFile = filepath.Join(dcrpoolHomeDir, defaultGUITLSCertFilename)
	defaultGUITLSKeyFile  = filepath.Join(dcrpoolHomeDir, defaultGUITLSKeyFilename)

	// This keypair is solely for client authentication to the wallet.
	defaultWalletTLSCertFile = filepath.Join(dcrpoolHomeDir, defaultWalletTLSCertFilename)
	defaultWalletTLSKeyFile  = filepath.Join(dcrpoolHomeDir, defaultWalletTLSKeyFilename)
)

// runServiceCommand is only set to a real function on Windows.  It is used
// to parse and execute service commands specified via the -s flag.
var runServiceCommand func(string) error

// config defines the configuration options for the pool.
type config struct {
	ShowVersion           bool          `long:"version" no-ini:"true" description:"Display version information and exit."`
	HomeDir               string        `long:"appdata" ini-name:"appdata" description:"Path to application home directory."`
	ConfigFile            string        `long:"configfile" ini-name:"configfile" description:"Path to configuration file."`
	DataDir               string        `long:"datadir" ini-name:"datadir" description:"The data directory."`
	ActiveNet             string        `long:"activenet" ini-name:"activenet" description:"The active network being mined on. {testnet3, mainnet, simnet}"`
	GUIListen             string        `long:"guilisten" ini-name:"guilisten" description:"The address:port for pool GUI listening."`
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
	PoolFee               float64       `long:"poolfee" ini-name:"poolfee" description:"The fee charged for pool participation. Minimum 0.002 (0.2%), maximum 0.05 (5%)."`
	MaxTxFeeReserve       float64       `long:"maxtxfeereserve" ini-name:"maxtxfeereserve" description:"DEPRECATED -- The maximum amount reserved for transaction fees, in DCR."`
	MaxGenTime            time.Duration `long:"maxgentime" ini-name:"maxgentime" description:"The share creation target time for the pool. Valid time units are {s,m,h}. Minimum 2 seconds. This currently should be below 30 seconds to increase the likelihood a work submission for clients between new work distributions by the pool."`
	PaymentMethod         string        `long:"paymentmethod" ini-name:"paymentmethod" description:"The payment method of the pool. {pps, pplns}"`
	LastNPeriod           time.Duration `long:"lastnperiod" ini-name:"lastnperiod" description:"The time period of interest when using PPLNS payment scheme. Valid time units are {s,m,h}. Minimum 60 seconds."`
	WalletPass            string        `long:"walletpass" ini-name:"walletpass" description:"The wallet passphrase to use when paying dividends to pool contributors."`
	WalletAccount         uint32        `long:"walletaccount" ini-name:"walletaccount" description:"The wallet account that will receive mining rewards when not mining as a solo pool."`
	MinPayment            float64       `long:"minpayment" ini-name:"minpayment" description:"DEPRECATED -- The minimum payment to process for an account."`
	SoloPool              bool          `long:"solopool" ini-name:"solopool" description:"Solo pool mode. This disables payment processing when enabled."`
	AdminPass             string        `long:"adminpass" ini-name:"adminpass" description:"The admin password."`
	GUIDir                string        `long:"guidir" ini-name:"guidir" description:"The path to the directory containing the pool's user interface assets (templates, css etc.)"`
	Domain                string        `long:"domain" ini-name:"domain" description:"The domain of the mining pool, required for TLS."`
	UseLEHTTPS            bool          `long:"uselehttps" ini-name:"uselehttps" description:"This enables HTTPS using a Letsencrypt certificate. By default the pool uses a self-signed certificate for HTTPS."`
	GUITLSCert            string        `long:"tlscert" ini-name:"tlscert" description:"Path to the TLS cert file (for running GUI on https)."`
	GUITLSKey             string        `long:"tlskey" ini-name:"tlskey" description:"Path to the TLS key file (for running GUI on https)."`
	WalletTLSCert         string        `long:"wallettlscert" ini-name:"wallettlscert" description:"Path to the wallet client TLS cert file."`
	WalletTLSKey          string        `long:"wallettlskey" ini-name:"wallettlskey" description:"Path to the wallet client TLS key file."`
	Designation           string        `long:"designation" ini-name:"designation" description:"The designated codename for this pool. Customises the logo in the top toolbar."`
	MaxConnectionsPerHost uint32        `long:"maxconnperhost" ini-name:"maxconnperhost" description:"The maximum number of connections allowed per host."`
	Profile               string        `long:"profile" ini-name:"profile" description:"Enable HTTP profiling on given [addr:]port -- NOTE port must be between 1024 and 65536"`
	MinerListen           string        `long:"minerlisten" ini-name:"minerlisten" description:"The address:port for miner connections."`
	CoinbaseConfTimeout   time.Duration `long:"conftimeout" ini-name:"conftimeout" description:"The duration to wait for coinbase confirmations."`
	GenCertsOnly          bool          `long:"gencertsonly" ini-name:"gencertsonly" description:"Only generate needed TLS key pairs and terminate."`
	UsePostgres           bool          `long:"postgres" ini-name:"postgres" description:"Use postgres database instead of bolt."`
	PGHost                string        `long:"postgreshost" ini-name:"postgreshost" description:"Host to establish a postgres connection."`
	PGPort                uint32        `long:"postgresport" ini-name:"postgresport" description:"Port to establish a postgres connection."`
	PGUser                string        `long:"postgresuser" ini-name:"postgresuser" description:"Username for postgres authentication."`
	PGPass                string        `long:"postgrespass" ini-name:"postgrespass" description:"Password for postgres authentication."`
	PGDBName              string        `long:"postgresdbname" ini-name:"postgresdbname" description:"Postgres database name."`
	PurgeDB               bool          `long:"purgedb" ini-name:"purgedb" description:"Wipes all existing data on startup for a postgres backend. This intended for simnet testing purposes only."`
	MonitorCycle          time.Duration `long:"monitorcycle" ini-name:"monitorcycle" description:"Time spent monitoring a mining client for possible upgrades."`
	MaxUpgradeTries       uint32        `long:"maxupgradetries" ini-name:"maxupgradetries" description:"Maximum consecuctive miner monitoring and upgrade tries."`
	NoGUITLS              bool          `long:"noguitls" ini-name:"noguitls" description:"Disable TLS on GUI endpoint (eg. for reverse proxy with a dedicated webserver)."`
	poolFeeAddrs          []stdaddr.Address
	dcrdRPCCerts          []byte
	net                   *params
	clientTimeout         time.Duration
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
	if err = os.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(keyFile, key, 0600); err != nil {
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
//  1. Start with a default config with sane settings
//  2. Pre-parse the command line to check for an alternative config file
//  3. Load configuration file overwriting defaults with any specified options
//  4. Parse CLI options and overwrite/add any specified options
//
// The above results in dcrpool functioning properly without any config settings
// while still allowing the user to override settings with config files and
// command line options.  Command line options always take precedence.
func loadConfig() (*config, []string, error) {
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
		MaxGenTime:            defaultMaxGenTime,
		ActiveNet:             defaultActiveNet,
		PaymentMethod:         defaultPaymentMethod,
		LastNPeriod:           defaultLastNPeriod,
		SoloPool:              defaultSoloPool,
		GUIListen:             defaultGUIListen,
		GUIDir:                defaultGUIDir,
		UseLEHTTPS:            defaultUseLEHTTPS,
		GUITLSCert:            defaultGUITLSCertFile,
		GUITLSKey:             defaultGUITLSKeyFile,
		WalletTLSCert:         defaultWalletTLSCertFile,
		WalletTLSKey:          defaultWalletTLSKeyFile,
		Designation:           defaultDesignation,
		MaxConnectionsPerHost: defaultMaxConnectionsPerHost,
		MinerListen:           defaultMinerListen,
		WalletAccount:         defaultWalletAccount,
		CoinbaseConfTimeout:   defaultCoinbaseConfTimeout,
		UsePostgres:           defaultUsePostgres,
		PGHost:                defaultPGHost,
		PGPort:                defaultPGPort,
		PGUser:                defaultPGUser,
		PGPass:                defaultPGPass,
		PGDBName:              defaultPGDBName,
		MonitorCycle:          defaultMonitorCycle,
		MaxUpgradeTries:       defaultMaxUpgradeTries,
		NoGUITLS:              defaultNoGUITLS,
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
		var e *flags.Error
		if errors.As(err, &e) {
			if e.Type != flags.ErrHelp {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			} else if e.Type == flags.ErrHelp {
				fmt.Fprintln(os.Stdout, err)
				os.Exit(0)
			}
		}
	}

	appName := filepath.Base(os.Args[0])

	// Show the version and exit if the version flag was specified.
	if preCfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n", appName,
			version(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

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
		if preCfg.GUITLSCert == defaultGUITLSCertFile {
			cfg.GUITLSCert = filepath.Join(cfg.HomeDir, defaultGUITLSCertFilename)
		} else {
			cfg.GUITLSCert = preCfg.GUITLSCert
		}
		if preCfg.GUITLSKey == defaultGUITLSKeyFile {
			cfg.GUITLSKey = filepath.Join(cfg.HomeDir, defaultGUITLSKeyFilename)
		} else {
			cfg.GUITLSKey = preCfg.GUITLSKey
		}
		if preCfg.WalletTLSCert == defaultWalletTLSCertFile {
			cfg.WalletTLSCert = filepath.Join(cfg.HomeDir,
				defaultWalletTLSCertFilename)
		} else {
			cfg.WalletTLSCert = preCfg.WalletTLSCert
		}
		if preCfg.WalletTLSKey == defaultWalletTLSKeyFile {
			cfg.WalletTLSKey = filepath.Join(cfg.HomeDir,
				defaultWalletTLSKeyFilename)
		} else {
			cfg.WalletTLSKey = preCfg.WalletTLSKey
		}
	}

	// Create the home directory if it doesn't already exist.
	const funcName = "loadConfig"
	err = os.MkdirAll(cfg.HomeDir, 0700)
	if err != nil {
		// Show a nicer error message if it's because a symlink is
		// linked to a directory that does not exist (probably because
		// it's not mounted).
		var e *os.PathError
		if errors.As(err, &e) && os.IsExist(err) {
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
			err = fmt.Errorf("error creating a default config file: %v", err)
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
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
		var e *os.PathError
		if !errors.As(err, &e) {
			fmt.Fprintf(os.Stderr, "error parsing config file: %v\n", err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}
		configFileError = err
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		var e *flags.Error
		if !errors.As(err, &e) || e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, usageMessage)
		}
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
		err := fmt.Errorf("unknown network provided: %v", cfg.ActiveNet)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Use separate data and log directories for each Decred network.
	cfg.DataDir = cleanAndExpandPath(filepath.Join(cfg.DataDir, cfg.net.Name))
	cfg.LogDir = cleanAndExpandPath(filepath.Join(cfg.LogDir, cfg.net.Name))

	logRotator = nil

	// Initialize log rotation.  After log rotation has been initialized, the
	// logger variables may be used.
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename))

	// Ensure the admin password is set.
	if cfg.AdminPass == "" {
		err := fmt.Errorf("the adminpass option is not set")
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Ensure the dcrd rpc username is set.
	if cfg.RPCUser == "" {
		err := fmt.Errorf("the rpcuser option is not set")
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Ensure the dcrd rpc password is set.
	if cfg.RPCPass == "" {
		err := fmt.Errorf("the rpcpass option is not set")
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Create the data directory.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		str := "%s: unable to create data directory (%s): %v"
		err := fmt.Errorf(str, funcName, cfg.DataDir, err)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	// Assert postgres config details are valid if being used.
	if cfg.UsePostgres {
		if cfg.PGHost == "" {
			err := fmt.Errorf("the postgreshost option is not set")
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		if cfg.PGUser == "" {
			err := fmt.Errorf("the postgresuser option is not set")
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		if cfg.PGDBName == "" {
			err := fmt.Errorf("the postgresdbname option is not set")
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}
	}

	// Special show command to list supported subsystems and exit.
	if cfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Add default ports for the active network if there are no ports specified.
	cfg.DcrdRPCHost = normalizeAddress(cfg.DcrdRPCHost, cfg.net.DcrdRPCServerPort)
	cfg.WalletGRPCHost = normalizeAddress(cfg.WalletGRPCHost, cfg.net.WalletGRPCServerPort)

	cfg.MinerListen = normalizeAddress(cfg.MinerListen, defaultMinerPort)
	cfg.GUIListen = normalizeAddress(cfg.GUIListen, defaultGUIPort)

	if !cfg.SoloPool {
		// Ensure a valid payment method is set.
		if cfg.PaymentMethod != pool.PPS && cfg.PaymentMethod != pool.PPLNS {
			err := fmt.Errorf("paymentmethod must be either %s or %s",
				pool.PPS, pool.PPLNS)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		// Ensure pool fee is valid.
		if cfg.PoolFee < 0.002 || cfg.PoolFee > 0.05 {
			err := fmt.Errorf("poolfee should be between 0.002 (0.2%%) " +
				"and 0.05 (5%%)")
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		// Ensure the passphrase to unlock the wallet is provided.
		// Wallet passphrase is required to pay dividends to pool contributors.
		if cfg.WalletPass == "" {
			err := fmt.Errorf("the walletpass option is not set")
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		// Ensure address to collect pool fees is provided.
		// jessevdk/go-flags does not automatically split the string, so at this
		// point either the array is empty, or the first item of the array
		// contains the full string.
		if len(cfg.PoolFeeAddrs) == 0 || len(cfg.PoolFeeAddrs[0]) == 0 {
			err := fmt.Errorf("the poolfeeaddrs option is not set")
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		// Split the string into an array, and parse pool fee addresses.
		cfg.PoolFeeAddrs = strings.Split(cfg.PoolFeeAddrs[0], ",")
		for _, pAddr := range cfg.PoolFeeAddrs {
			addr, err := stdaddr.DecodeAddress(pAddr, cfg.net)
			if err != nil {
				err := fmt.Errorf("unable to decode pool fee address '%v': "+
					"%v", pAddr, err)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return nil, nil, err
			}

			cfg.poolFeeAddrs = append(cfg.poolFeeAddrs, addr)
		}
	}

	// Do not allow maxgentime durations that are too short.
	if cfg.MaxGenTime < time.Second*2 {
		str := "the maxgentime option may not be less " +
			"than 2s -- parsed [%v]"
		err := fmt.Errorf(str, cfg.MaxGenTime)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Do not allow lastnperiod durations that are too short.
	if cfg.LastNPeriod < time.Second*60 {
		str := "the lastnperiod option may not be less " +
			"than 60s -- parsed [%v]"
		err := fmt.Errorf(str, cfg.LastNPeriod)
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

	if cfg.NoGUITLS && cfg.UseLEHTTPS {
		err := fmt.Errorf("only one of uselehttps and noguitls can be specified")
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Ensure a domain is set if HTTPS via letsencrypt is preferred.
	if cfg.UseLEHTTPS && cfg.Domain == "" {
		err := fmt.Errorf("a valid domain is required for HTTPS " +
			"via letsencrypt")
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Generate self-signed TLS cert and key if they do not already exist. This
	// keypair is solely for enabling HTTPS connections to the pool's
	// web interface.
	if !cfg.UseLEHTTPS && (!fileExists(cfg.GUITLSCert) || !fileExists(cfg.GUITLSKey)) {
		err := genCertPair(cfg.GUITLSCert, cfg.GUITLSKey)
		if err != nil {
			str := "%s: unable to generate dcrpool's TLS cert/key: %v"
			err := fmt.Errorf(str, funcName, err)
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err

		}
	}

	// Load dcrd RPC certificate.
	if !fileExists(cfg.DcrdRPCCert) {
		str := "%s: dcrd RPC certificate (%v) not found"
		err := fmt.Errorf(str, funcName, cfg.DcrdRPCCert)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	cfg.dcrdRPCCerts, err = os.ReadFile(cfg.DcrdRPCCert)
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
			err := fmt.Errorf("invalid profile address: %s", err)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}

		// Finally, check the port is in range.
		if port, _ := strconv.Atoi(portStr); port < 1024 || port > 65535 {
			err := fmt.Errorf("profile address (%s) port must be "+
				"between 1024 and 65535", cfg.Profile)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}
	}

	if !cfg.SoloPool {
		// Load the wallet RPC certificate.
		if !cfg.GenCertsOnly && !fileExists(cfg.WalletRPCCert) {
			err := fmt.Errorf("wallet RPC certificate (%v) not found",
				cfg.WalletRPCCert)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err

		}

		// Generate self-signed wallet TLS cert and key if they do not
		// already exist. This keypair is solely for client authentication
		// to the wallet.
		if !fileExists(cfg.WalletTLSCert) || !fileExists(cfg.WalletTLSKey) {
			err := genCertPair(cfg.WalletTLSCert, cfg.WalletTLSKey)
			if err != nil {
				err := fmt.Errorf("failed to generate dcrpool's wallet TLS "+
					"cert/key: %v", err)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return nil, nil, err

			}
		}
	}

	if cfg.ActiveNet != chaincfg.SimNetParams().Name && cfg.PurgeDB {
		err := fmt.Errorf("database purging at startup is " +
			"reserved for simnet testing only")
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Warn about deprecated config options if they have been set.
	if cfg.MaxTxFeeReserve > 0 {
		str := "maxtxfeereserve has been deprecated, " +
			"please remove from your config file."
		mpLog.Warnf(str, funcName)
	}

	if cfg.MinPayment > 0 {
		str := "minpayment has been deprecated, " +
			"please remove from your config file."
		mpLog.Warnf(str, funcName)
	}

	// Only generate needed key pairs and terminate if GenCertsOnly is active.
	if cfg.GenCertsOnly {
		err := errors.New("generated needed certificates, " +
			"terminating.")
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Define the client timeout to be approximately four block times
	// per the active network, except for simnet.
	switch cfg.ActiveNet {
	case chaincfg.TestNet3Params().Name, chaincfg.MainNetParams().Name:
		cfg.clientTimeout = cfg.net.TargetTimePerBlock * 4
	default:
		cfg.clientTimeout = time.Second * 30
	}

	return &cfg, remainingArgs, nil
}
