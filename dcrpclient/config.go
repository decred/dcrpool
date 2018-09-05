package main

import (
	"crypto/elliptic"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/decred/dcrd/certgen"
	"github.com/decred/slog"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultLogLevel        = "debug"
	defaultConfigFilename  = "dcrpclient.conf"
	defaultHost            = "localhost:25000"
	defaultDataDirname     = "data"
	defaultEndpoint        = "wss"
	defaultTLSCertFilename = "tls.cert"
	defaultTLSKeyFilename  = "tls.key"
	defaultLogDirname      = "log"
	defaultLogFilename     = "client.log"
)

var (
	defaultHomeDir     = "deploy" // dcrutil.AppDataDir("dcrpclient", false)
	defaultDataDir     = filepath.Join(defaultHomeDir, defaultDataDirname)
	defaultConfigFile  = filepath.Join(defaultHomeDir, defaultConfigFilename)
	defaultTLSCertFile = filepath.Join(defaultDataDir, defaultTLSCertFilename)
	defaultTLSKeyFile  = filepath.Join(defaultDataDir, defaultTLSKeyFilename)
	defaultLogDir      = filepath.Join(defaultHomeDir, defaultLogDirname)
)

// runServiceCommand is only set to a real function on Windows.  It is used
// to parse and execute service commands specified via the -s flag.
var runServiceCommand func(string) error

// config describes the connection parameters for the client.
type config struct {
	HomeDir     string `long:"homedir" description:"Path to application home directory"`
	ConfigFile  string `long:"configfile" description:"Path to configuration file"`
	Host        string `long:"host" description:"The ip address and port of the mining pool"`
	DataDir     string `long:"datadir" description:"The data directory"`
	DebugLevel  string `long:"debuglevel" description:"Logging level for all subsystems {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems"`
	LogDir      string `long:"logdir" description:"Directory to log output."`
	TLSCert     string `long:"tlscert" description:"The TLS certificate file"`
	TLSKey      string `long:"tlskey" description:"The TLS private key"`
	certificate tls.Certificate
}

// serviceOptions defines the configuration options for the daemon as a service on
// Windows.
type serviceOptions struct {
	ServiceCommand string `short:"s" long:"service" description:"Service command {install, remove, start, stop}"`
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

// createConfigFile copies the sample config to the given destination path.
func createConfigFile(preCfg config) error {
	// Create the destination directory if it does not exist.
	err := os.MkdirAll(filepath.Dir(preCfg.ConfigFile), 0700)
	if err != nil {
		return err
	}

	// Replace the sample configuration file contents with the provided values.
	debugLevelRE := regexp.MustCompile(`(?m)^;\s*debuglevel=[^\s]*$`)
	homeDirRE := regexp.MustCompile(`(?m)^;\s*homedir=[^\s]*$`)
	dataDirRE := regexp.MustCompile(`(?m)^;\s*datadir=[^\s]*$`)
	configFileRE := regexp.MustCompile(`(?m)^;\s*configfile=[^\s]*$`)
	logDirRE := regexp.MustCompile(`(?m)^;\s*logdir=[^\s]*$`)
	hostRE := regexp.MustCompile(`(?m)^;\s*host=[^\s]*$`)
	tlsCertRE := regexp.MustCompile(`(?m)^;\s*tlscert=[^\s]*$`)
	tlsKeyRE := regexp.MustCompile(`(?m)^;\s*tlskey=[^\s]*$`)
	s := homeDirRE.ReplaceAllString(ConfigFileContents, fmt.Sprintf("homedir=%s", preCfg.HomeDir))
	s = debugLevelRE.ReplaceAllString(s, fmt.Sprintf("debuglevel=%s", preCfg.DebugLevel))
	s = dataDirRE.ReplaceAllString(s, fmt.Sprintf("datadir=%s", preCfg.DataDir))
	s = configFileRE.ReplaceAllString(s, fmt.Sprintf("configfile=%s", preCfg.ConfigFile))
	s = hostRE.ReplaceAllString(s, fmt.Sprintf("dbfile=%s", preCfg.Host))
	s = logDirRE.ReplaceAllString(s, fmt.Sprintf("logdir=%s", preCfg.LogDir))
	s = tlsCertRE.ReplaceAllString(s, fmt.Sprintf("tlscert=%s", preCfg.TLSCert))
	s = tlsKeyRE.ReplaceAllString(s, fmt.Sprintf("tlskey=%s", preCfg.TLSKey))

	// Create config file at the provided path.
	dest, err := os.OpenFile(preCfg.ConfigFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = dest.WriteString(s)
	return err
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
		ConfigFile: defaultConfigFile,
		Host:       defaultHost,
		DataDir:    defaultDataDir,
		TLSCert:    defaultTLSCertFile,
		TLSKey:     defaultTLSKeyFile,
		DebugLevel: defaultLogLevel,
		LogDir:     defaultLogDir,
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
		if preCfg.DataDir == defaultDataDir {
			cfg.DataDir = filepath.Join(cfg.HomeDir, defaultDataDirname)
		} else {
			cfg.DataDir = preCfg.DataDir
		}
		if preCfg.TLSCert == defaultTLSCertFile {
			cfg.TLSCert = filepath.Join(cfg.DataDir, defaultTLSCertFilename)
		} else {
			cfg.TLSCert = preCfg.TLSCert
		}
		if preCfg.TLSKey == defaultTLSKeyFile {
			cfg.TLSKey = filepath.Join(cfg.DataDir, defaultTLSKeyFilename)
		} else {
			cfg.TLSKey = preCfg.TLSKey
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
		err := createConfigFile(preCfg)
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

	cfg.DataDir = cleanAndExpandPath(cfg.DataDir)
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

	// Warn about missing config file only after all other configuration is
	// done. This prevents the warning on help messages and invalid
	// options. Note this should go directly before the return.
	if configFileError != nil {
		log.Debugf("%v", configFileError)
	}

	// Create TLS certificate and key if the certificate does not exist.
	if !fileExists(cfg.TLSCert) {
		err := os.MkdirAll(cfg.DataDir, 0700)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
			os.Exit(1)
		}

		// Generated certificate will have 5 years of validity.
		validUntil := time.Now().Add(time.Duration(5) * 365 * 24 * time.Hour)
		cert, key, err := certgen.NewTLSCertPair(elliptic.P521(),
			"dcrpclient", validUntil, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create TLS certificate "+
				"file %v", err)
		}

		// Write cert and key files.
		if err = ioutil.WriteFile(cfg.TLSCert, cert, 0644); err != nil {
			return nil, nil, fmt.Errorf("cannot write TLS cert: %v", err)
		}

		if err = ioutil.WriteFile(cfg.TLSKey, key, 0600); err != nil {
			os.Remove(cfg.TLSCert)
			return nil, nil, fmt.Errorf("cannot write TLS key: %v", err)
		}
	}

	keypair, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
	if err != nil {
		return nil, nil, err
	}

	cfg.certificate = keypair

	return &cfg, remainingArgs, nil
}
