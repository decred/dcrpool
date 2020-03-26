// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrpool/pool"
)

// Config contains all of the required configuration values for the GUI
// component
type Config struct {
	// SoloPool represents the solo pool mining mode.
	SoloPool bool
	// PaymentMethod represents the pool payment method.
	PaymentMethod string
	// GUIDir represents the GUI directory.
	GUIDir string
	// CSRFSecret represents the frontend's CSRF secret.
	CSRFSecret []byte
	// AdminPass represents the admin password.
	AdminPass string
	// GUIPort represents the port the frontend is served on.
	GUIPort uint32
	// TLSCertFile represents the TLS certificate file path.
	TLSCertFile string
	// TLSKeyFile represents the TLS key file path.
	TLSKeyFile string
	// UseLEHTTPS represents Letsencrypt HTTPS mode.
	UseLEHTTPS bool
	// Domain represents the domain name of the pool.
	Domain string
	// ActiveNet represents the active network being mined on.
	ActiveNet *chaincfg.Params
	// BlockExplorerURL represents the active network block explorer.
	BlockExplorerURL string
	// Designation represents the codename of the pool.
	Designation string
	// PoolFee represents the fee charged to participating accounts of the pool.
	PoolFee float64
	// MinerPorts represents the configured ports for supported miners.
	MinerPorts map[string]uint32
	// WithinLimit returns if a client is within its request limits.
	WithinLimit func(string, int) bool
	// FetchLastWorkHeight returns the last work height of the pool.
	FetchLastWorkHeight func() uint32
	// FetchLastPaymentheight returns the last payment height of the pool.
	FetchLastPaymentHeight func() uint32
	// AddPaymentRequest creates a payment request from the provided account
	// if not already requested.
	AddPaymentRequest func(addr string) error
	// FetchMinedWork returns the last ten mined blocks by the pool.
	FetchMinedWork func() ([]*pool.AcceptedWork, error)
	// FetchWorkQuotas returns the reward distribution to pool accounts
	// based on work contributed per the payment scheme used by the pool.
	FetchWorkQuotas func() ([]*pool.Quota, error)
	// FetchPoolHashRate returns the hash rate of the pool.
	FetchPoolHashRate func() (*big.Rat, map[string][]*pool.ClientInfo)
	// BackupDB streams a backup of the database over an http response.
	BackupDB func(w http.ResponseWriter) error
	// FetchClientInfo returns connection details about all pool clients.
	FetchClientInfo func() map[string][]*pool.ClientInfo
	// AccountExists checks if the provided account id references a pool account.
	AccountExists func(accountID string) bool
	// FetchMinedWorkByAccount returns a list of mined work by the provided address.
	FetchMinedWorkByAccount func(id string) ([]*pool.AcceptedWork, error)
	// FetchPaymentsForAccount returns a list or payments made to the provided address.
	FetchPaymentsForAccount func(id string) ([]*pool.Payment, error)
	// FetchAccountClientInfo returns all clients belonging to the provided
	// account id.
	FetchAccountClientInfo func(accountID string) []*pool.ClientInfo
}

// GUI represents the the mining pool user interface.
type GUI struct {
	cfg         *Config
	csrfSecret  []byte
	limiter     *pool.RateLimiter
	templates   *template.Template
	cookieStore *sessions.CookieStore
	router      *mux.Router
	server      *http.Server

	// The following fields cache pool data.
	minedWork     []minedWork
	minedWorkMtx  sync.RWMutex
	workQuotas    []workQuota
	workQuotasMtx sync.RWMutex
	poolHash      string
	poolHashMtx   sync.RWMutex
}

type poolStats struct {
	SoloPool          bool
	PoolFee           float64
	Network           string
	PaymentMethod     string
	LastWorkHeight    uint32
	LastPaymentHeight uint32
	PoolHashRate      string
}

// route configures the http router of the user interface.
func (ui *GUI) route() {
	ui.router = mux.NewRouter()
	ui.router.Use(csrf.Protect(ui.cfg.CSRFSecret, csrf.Secure(true)))

	cssDir := http.Dir(filepath.Join(ui.cfg.GUIDir, "assets/public/css"))
	ui.router.PathPrefix("/css/").Handler(http.StripPrefix("/css/",
		http.FileServer(cssDir)))

	imagesDir := http.Dir(filepath.Join(ui.cfg.GUIDir, "assets/public/images"))
	ui.router.PathPrefix("/images/").Handler(http.StripPrefix("/images/",
		http.FileServer(imagesDir)))

	jsDir := http.Dir(filepath.Join(ui.cfg.GUIDir, "assets/public/js"))
	ui.router.PathPrefix("/js/").Handler(http.StripPrefix("/js/",
		http.FileServer(jsDir)))

	ui.router.HandleFunc("/", ui.Homepage).Methods("GET")
	ui.router.HandleFunc("/account", ui.IsPoolAccount).Methods("GET")
	ui.router.HandleFunc("/admin", ui.AdminPage).Methods("GET")
	ui.router.HandleFunc("/admin", ui.AdminLogin).Methods("POST")
	ui.router.HandleFunc("/backup", ui.DownloadDatabaseBackup).Methods("POST")
	ui.router.HandleFunc("/logout", ui.AdminLogout).Methods("POST")

	// Websocket endpoint allows the GUI to receive updated values
	ui.router.HandleFunc("/ws", ui.registerWebSocket).Methods("GET")
}

// renderTemplate executes the provided template.
func (ui *GUI) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	var doc bytes.Buffer
	err := ui.templates.ExecuteTemplate(&doc, name, data)
	if err != nil {
		log.Errorf("template error: %v", err)
		http.Error(w, "template error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}
	_, err = doc.WriteTo(w)
	if err != nil {
		log.Errorf("unable to render template: %v", err)
	}
}

func getSession(r *http.Request, cookieStore *sessions.CookieStore) (*sessions.Session, error) {
	session, err := cookieStore.Get(r, "session")
	if err != nil {
		// "value is not valid" occurs if the CSRF secret changes.
		// This is common during development (eg. when using the test harness)
		// but it should not occur in production.
		if strings.Contains(err.Error(), "securecookie: the value is not valid") {
			log.Warnf("getSession error: CSRF secret has changed. Generating new session.")
			err = nil
		}
	}
	return session, err
}

// NewGUI creates an instance of the user interface.
func NewGUI(cfg *Config) (*GUI, error) {
	ui := &GUI{
		cfg:        cfg,
		limiter:    pool.NewRateLimiter(),
		minedWork:  make([]minedWork, 0),
		workQuotas: make([]workQuota, 0),
	}

	switch cfg.ActiveNet.Name {
	case chaincfg.TestNet3Params().Name:
		ui.cfg.BlockExplorerURL = "https://testnet.dcrdata.org"
	case chaincfg.SimNetParams().Name:
		ui.cfg.BlockExplorerURL = "..."
	default:
		ui.cfg.BlockExplorerURL = "https://dcrdata.decred.org"
	}

	ui.cookieStore = sessions.NewCookieStore(cfg.CSRFSecret)

	err := ui.loadTemplates()
	if err != nil {
		return nil, err
	}

	ui.route()

	return ui, nil
}

// loadTemplates initializes the html templates of the pool user interface.
func (ui *GUI) loadTemplates() error {
	var templates []string
	findTemplate := func(path string, f os.FileInfo, err error) error {
		// If path doesn't exist, or other error with path, return error so
		// that Walk will quit and return the error to the caller.
		if err != nil {
			return err
		}
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".html") {
			templates = append(templates, path)
		}
		return nil
	}

	err := filepath.Walk(ui.cfg.GUIDir, findTemplate)
	if err != nil {
		return err
	}

	httpTemplates := template.New("template").Funcs(template.FuncMap{
		"hashString":        hashString,
		"upper":             strings.ToUpper,
		"ratToPercent":      ratToPercent,
		"floatToPercent":    floatToPercent,
		"time":              formatUnixTime,
		"truncateAccountID": truncateAccountID,
		"blockURL":          blockURL,
		"txURL":             txURL,
	})

	// Since template.Must panics with non-nil error, it is much more
	// informative to pass the error to the caller to log it and exit
	// gracefully.
	httpTemplates, err = httpTemplates.ParseFiles(templates...)
	if err != nil {
		return err
	}

	ui.templates = template.Must(httpTemplates, nil)
	return nil
}

// Run starts the user interface.
func (ui *GUI) Run(ctx context.Context) {
	go func() {
		if !ui.cfg.UseLEHTTPS {
			log.Infof("Starting GUI server on port %d (https)", ui.cfg.GUIPort)
			ui.server = &http.Server{
				WriteTimeout: time.Second * 30,
				ReadTimeout:  time.Second * 30,
				IdleTimeout:  time.Second * 30,
				Addr:         fmt.Sprintf("0.0.0.0:%v", ui.cfg.GUIPort),
				Handler:      ui.router,
			}

			if err := ui.server.ListenAndServeTLS(ui.cfg.TLSCertFile,
				ui.cfg.TLSKeyFile); err != nil &&
				err != http.ErrServerClosed {
				log.Error(err)
			}
		}

		if ui.cfg.UseLEHTTPS {
			certCache := autocert.DirCache("certs")
			certMgr := &autocert.Manager{
				Prompt:     autocert.AcceptTOS,
				Cache:      certCache,
				HostPolicy: autocert.HostWhitelist(ui.cfg.Domain),
			}

			// Ensure port 80 is not already in use.
			port80 := ":80"
			listener, err := net.Listen("tcp", port80)
			if err != nil {
				log.Error("port 80 is already in use")
				return
			}

			listener.Close()

			// Redirect all regular http requests to their https endpoints.
			go func() {
				log.Info("Starting GUI server on port 80 (http, will forward to https)")
				if err := http.ListenAndServe(port80,
					certMgr.HTTPHandler(nil)); err != nil &&
					err != http.ErrServerClosed {
					log.Error(err)
				}
			}()

			log.Info("Starting GUI server on port 443 (https)")
			ui.server = &http.Server{
				WriteTimeout: time.Second * 30,
				ReadTimeout:  time.Second * 30,
				IdleTimeout:  time.Second * 30,
				Addr:         ":https",
				Handler:      ui.router,
				TLSConfig: &tls.Config{
					GetCertificate: certMgr.GetCertificate,
					MinVersion:     tls.VersionTLS12,
					CipherSuites: []uint16{
						tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
						tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					},
				},
			}

			if err := ui.server.ListenAndServeTLS("", ""); err != nil {
				log.Error(err)
			}
		}
	}()

	// Use a ticker to push updates through the socket periodically
	go func(ctx context.Context) {
		var ticks uint32
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ticks++

				// After three ticks (15 seconds) update cached pool data.
				if ticks == 3 {
					var err error
					work, err := ui.cfg.FetchMinedWork()
					if err != nil {
						log.Error(err)
						continue
					}

					// Parse and format mined work by the pool.
					workData := make([]minedWork, 0)
					for _, work := range work {
						workData = append(workData, minedWork{
							BlockHeight: work.Height,
							BlockURL:    blockURL(ui.cfg.BlockExplorerURL, work.Height),
							MinedBy:     truncateAccountID(work.MinedBy),
							Miner:       work.Miner,
						})
					}

					// Update mined work cache.
					ui.minedWorkMtx.Lock()
					ui.minedWork = workData
					ui.minedWorkMtx.Unlock()

					quotas, err := ui.cfg.FetchWorkQuotas()
					if err != nil {
						log.Error(err)
						continue
					}

					// Parse and format work quotas of the pool.
					quotaData := make([]workQuota, 0)
					for _, quota := range quotas {
						quotaData = append(quotaData, workQuota{
							AccountID: truncateAccountID(quota.AccountID),
							Percent:   ratToPercent(quota.Percentage),
						})
					}

					// Update work quotas cache.
					ui.workQuotasMtx.Lock()
					ui.workQuotas = quotaData
					ui.workQuotasMtx.Unlock()

					poolHash, _ := ui.cfg.FetchPoolHashRate()

					// Update pool hash cache.
					ui.poolHashMtx.Lock()
					ui.poolHash = hashString(poolHash)
					ui.poolHashMtx.Unlock()

					ticks = 0
				}

				ui.updateWebSocket()

			case <-ctx.Done():
				return
			}
		}
	}(ctx)
}
