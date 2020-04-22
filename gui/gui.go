// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	// IsPaymentRequested checks if a payment request has already been requested
	// for the account.
	IsPaymentRequested func(addr string) bool
	// FetchMinedWork returns all blocks mined by the pool.
	FetchMinedWork func() ([]*pool.AcceptedWork, error)
	// FetchWorkQuotas returns the reward distribution to pool accounts
	// based on work contributed per the payment scheme used by the pool.
	FetchWorkQuotas func() ([]*pool.Quota, error)
	// BackupDB streams a backup of the database over an http response.
	BackupDB func(w http.ResponseWriter) error
	// FetchClients returns all connected pool clients.
	FetchClients func() []*pool.Client
	// AccountExists checks if the provided account id references a pool account.
	AccountExists func(accountID string) bool
	// FetchArchivedPayments fetches all paid payments.
	FetchArchivedPayments func() ([]*pool.Payment, error)
	// FetchPendingPayments fetches all unpaid payments.
	FetchPendingPayments func() ([]*pool.Payment, error)
	// FetchCacheChannel returns the gui cache signal channel.
	FetchCacheChannel func() chan pool.CacheUpdateEvent
}

// GUI represents the the mining pool user interface.
type GUI struct {
	cfg         *Config
	limiter     *pool.RateLimiter
	templates   *template.Template
	cookieStore *sessions.CookieStore
	router      *mux.Router
	server      *http.Server
	cache       *Cache
}

// poolStatsData contains all of the necessary information to render the
// pool-stats template.
type poolStatsData struct {
	SoloPool          bool
	PoolFee           float64
	Network           string
	PaymentMethod     string
	LastWorkHeight    uint32
	LastPaymentHeight uint32
	PoolHashRate      string
}

// headerData contains all of the necessary information to render the
// header template.
type headerData struct {
	CSRF        template.HTML
	Designation string
	ShowMenu    bool
}

// route configures the http router of the user interface.
func (ui *GUI) route() {
	ui.router = mux.NewRouter()

	// Use a separate router without rate limiting (or other restrictions) for
	// static assets.
	assetsRouter := ui.router.PathPrefix("/assets").Subrouter()

	assetsDir := http.Dir(filepath.Join(ui.cfg.GUIDir, "assets/public/"))
	assetsRouter.PathPrefix("/").Handler(http.StripPrefix("/assets",
		http.FileServer(assetsDir)))

	// All other routes have rate limiting and CSRF protection applied.
	guiRouter := ui.router.PathPrefix("/").Subrouter()

	// sessionMiddleware must be run before rateLimitMiddleware.
	guiRouter.Use(ui.sessionMiddleware)
	guiRouter.Use(ui.rateLimitMiddleware)
	guiRouter.Use(csrf.Protect(ui.cfg.CSRFSecret, csrf.Secure(true)))

	guiRouter.HandleFunc("/", ui.homepage).Methods("GET")
	guiRouter.HandleFunc("/account", ui.account).Methods("GET")
	guiRouter.HandleFunc("/account", ui.isPoolAccount).Methods("HEAD")
	guiRouter.HandleFunc("/requestpayment", ui.requestPayment).Methods("POST")
	guiRouter.HandleFunc("/admin", ui.adminPage).Methods("GET")
	guiRouter.HandleFunc("/admin", ui.adminLogin).Methods("POST")
	guiRouter.HandleFunc("/backup", ui.downloadDatabaseBackup).Methods("POST")
	guiRouter.HandleFunc("/logout", ui.adminLogout).Methods("POST")

	// Paginated endpoints allow the GUI to request pages of data.
	guiRouter.HandleFunc("/blocks", ui.paginatedBlocks).Methods("GET")
	guiRouter.HandleFunc("/rewardquotas", ui.paginatedRewardQuotas).Methods("GET")
	guiRouter.HandleFunc("/account/{accountID}/blocks", ui.paginatedBlocksByAccount).Methods("GET")
	guiRouter.HandleFunc("/account/{accountID}/clients", ui.paginatedClientsByAccount).Methods("GET")
	guiRouter.HandleFunc("/account/{accountID}/payments/pending", ui.paginatedPendingPaymentsByAccount).Methods("GET")
	guiRouter.HandleFunc("/account/{accountID}/payments/archived", ui.paginatedArchivedPaymentsByAccount).Methods("GET")

	// Websocket endpoint allows the GUI to receive updated values.
	guiRouter.HandleFunc("/ws", ui.registerWebSocket).Methods("GET")
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

// NewGUI creates an instance of the user interface.
func NewGUI(cfg *Config) (*GUI, error) {
	ui := &GUI{
		cfg:     cfg,
		limiter: pool.NewRateLimiter(),
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
		"upper":          strings.ToUpper,
		"floatToPercent": floatToPercent,
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

	// Initalise the cache.
	work, err := ui.cfg.FetchMinedWork()
	if err != nil {
		log.Error(err)
		return
	}

	quotas, err := ui.cfg.FetchWorkQuotas()
	if err != nil {
		log.Error(err)
		return
	}

	clients := ui.cfg.FetchClients()

	pendingPayments, err := ui.cfg.FetchPendingPayments()
	if err != nil {
		log.Error(err)
		return
	}

	archivedPayments, err := ui.cfg.FetchArchivedPayments()
	if err != nil {
		log.Error(err)
		return
	}

	ui.cache = InitCache(work, quotas, clients, pendingPayments, archivedPayments, ui.cfg.BlockExplorerURL)

	// Use a ticker to periodically update cached data and push updates through
	// any established websockets
	go func(ctx context.Context) {
		signalCh := ui.cfg.FetchCacheChannel()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				clients := ui.cfg.FetchClients()
				ui.cache.updateClients(clients)
				ui.updateWebSocket()

			case msg := <-signalCh:
				switch msg {
				case pool.Confirmed, pool.Unconfirmed:
					work, err := ui.cfg.FetchMinedWork()
					if err != nil {
						log.Error(err)
						continue
					}

					ui.cache.updateMinedWork(work)
					ui.updateWebSocket()

				case pool.ConnectedClient, pool.DisconnectedClient:
					// Opting to keep connection updates pushed by the ticker
					// to avoid pushing too much too frequently.
					clients := ui.cfg.FetchClients()
					ui.cache.updateClients(clients)

				case pool.ClaimedShare:
					quotas, err := ui.cfg.FetchWorkQuotas()
					if err != nil {
						log.Error(err)
						continue
					}

					ui.cache.updateRewardQuotas(quotas)
					ui.updateWebSocket()

				case pool.DividendsPaid:
					pendingPayments, err := ui.cfg.FetchPendingPayments()
					if err != nil {
						log.Error(err)
						continue
					}

					archivedPayments, err := ui.cfg.FetchArchivedPayments()
					if err != nil {
						log.Error(err)
						continue
					}

					ui.cache.updatePayments(pendingPayments, archivedPayments)
					ui.updateWebSocket()

				default:
					log.Errorf("unknown cache signal received: %v", msg)
				}

			case <-ctx.Done():
				return
			}
		}
	}(ctx)
}
