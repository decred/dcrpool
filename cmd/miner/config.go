// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"net"
	_ "net/http/pprof"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/slog"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultLogLevel       = "debug"
	defaultConfigFilename = "miner.conf"
	defaultLogDirname     = "log"
	defaultLogFilename    = "miner.log"
)

var (
	defaultActiveNet  = chaincfg.SimNetParams().Name
	defaultHomeDir    = dcrutil.AppDataDir("miner", false)
	defaultConfigFile = filepath.Join(defaultHomeDir, defaultConfigFilename)
	defaultLogDir     = filepath.Join(defaultHomeDir, defaultLogDirname)
	defaultUserAgent  = fmt.Sprintf("%s/%s", "cpuminer", minerVersion())
)

// runServiceCommand is only set to a real function on Windows.  It is used
// to parse and execute service commands specified via the -s flag.
var runServiceCommand func(string) error

// config describes the connection parameters for the client.
type config struct {
	HomeDir    string `long:"appdata" ini-name:"appdata" description:"Path to application home directory."`
	ConfigFile string `long:"configfile" ini-name:"configfile" description:"Path to configuration file."`
	ActiveNet  string `long:"activenet" ini-name:"activenet" description:"The active network being mined on. {simnet, testnet3, mainnet}"`
	User       string `long:"user" ini-name:"user" description:"The username of the mining account."`
	Address    string `long:"address" ini-name:"address" description:"The address of the mining account."`
	Pool       string `long:"pool" ini-name:"pool" description:"The stratum domain and port of the mining pool to connect to. eg. dcrpool.com:4445."`
	DebugLevel string `long:"debuglevel" ini-name:"debuglevel" description:"Logging level for all subsystems {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems."`
	LogDir     string `long:"logdir" ini-name:"logdir" description:"The log output directory."`
	MaxProcs   int    `long:"maxprocs" ini-name:"maxprocs" description:"Number of CPU cores to use. Default is all cores."`
	Profile    string `long:"profile" ini-name:"profile" description:"Enable HTTP profiling on given [addr:]port -- NOTE port must be between 1024 and 65536."`
	Stall      bool   `long:"stall" ini-name:"stall" description:"Do not generate work submissions."`
	UserAgent  string `long:"useragent" ini-name:"useragent" description:"The user agent to identify as in a subscription message."`

	net *chaincfg.Params
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
		HomeDir:    defaultHomeDir,
		ActiveNet:  defaultActiveNet,
		ConfigFile: defaultConfigFile,
		DebugLevel: defaultLogLevel,
		LogDir:     defaultLogDir,
		UserAgent:  defaultUserAgent,
		User:       "",
		Address:    "",
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
			} else {
				fmt.Fprintln(os.Stdout, err)
				os.Exit(0)
			}
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

	// Update the home directory for dcrpcclient if specified. Since the home
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
		if preCfg.ConfigFile == defaultConfigFile {
			defaultConfigFile = filepath.Join(cfg.HomeDir,
				defaultConfigFilename)
			preCfg.ConfigFile = defaultConfigFile
			cfg.ConfigFile = defaultConfigFile
		} else {
			cfg.ConfigFile = preCfg.ConfigFile
		}
		if preCfg.LogDir == defaultLogDir {
			cfg.LogDir = filepath.Join(cfg.HomeDir, defaultLogDirname)
		} else {
			cfg.LogDir = preCfg.LogDir
		}
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
	parser, err := newConfigParser(&cfg, &serviceOpts, flags.Default)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if preCfg.ConfigFile != defaultConfigFile {
		err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
		if err != nil {
			var e *os.PathError
			if !errors.As(err, &e) {
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
		var e *flags.Error
		if !errors.As(err, &e) || e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, usageMessage)
		}
		return nil, nil, err
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

	cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	logRotator = nil

	// Initialize log rotation.  After log rotation has been initialized, the
	// logger variables may be used.
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename))

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

	// Set the active network.
	switch cfg.ActiveNet {
	case chaincfg.SimNetParams().Name:
		cfg.net = chaincfg.SimNetParams()
	case chaincfg.TestNet3Params().Name:
		cfg.net = chaincfg.TestNet3Params()
	case chaincfg.MainNetParams().Name:
		cfg.net = chaincfg.MainNetParams()
	default:
		return nil, nil, fmt.Errorf("unknown network provided %v",
			cfg.ActiveNet)
	}

	// Validate format of profile, can be an address:port, or just a port.
	if cfg.Profile != "" {
		// If profile is just a number, then add a default host of "127.0.0.1"
		// such that Profile is a valid tcp address.
		if _, err := strconv.Atoi(cfg.Profile); err == nil {
			cfg.Profile = net.JoinHostPort("127.0.0.1", cfg.Profile)
		}

		// Check the Profile is a valid address
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

	if cfg.Pool == "" {
		str := "%s: the pool address cannot be an empty string"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Sanitize the pool address if it has the stratum prefix.
	cfg.Pool = strings.Replace(cfg.Pool, "stratum+tcp://", "", 1)

	// Check the pool addres is a valid address.
	_, poolPort, err := net.SplitHostPort(cfg.Pool)
	if err != nil {
		str := "%s: invalid pool address, %s"
		err := fmt.Errorf(str, funcName, err)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Finally, check the pool port is in range.
	if port, _ := strconv.Atoi(poolPort); port < 1024 || port > 65535 {
		str := "%s: profile: address %s: port must be between 1024 and 65535"
		err := fmt.Errorf(str, funcName, cfg.Profile)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	availableCPUs := runtime.NumCPU()
	if cfg.MaxProcs < 1 || cfg.MaxProcs > availableCPUs {
		log.Warnf("%d is not a valid value for MaxProcs. Defaulting to %d.", cfg.MaxProcs, availableCPUs)
		cfg.MaxProcs = availableCPUs
	}

	// Warn about missing config file only after all other configuration is
	// done. This prevents the warning on help messages and invalid
	// options. Note this should go directly before the return.
	if configFileError != nil {
		log.Warnf("%v", configFileError)
	}

	return &cfg, remainingArgs, nil
}
