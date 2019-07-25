// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"bytes"
	"context"
	"crypto/rand"
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

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrpool/pool"
)

// Config represents configuration details for the pool user interface.
type Config struct {
	Ctx              context.Context
	SoloPool         bool
	PaymentMethod    string
	GUIDir           string
	CSRFSecret       []byte
	BackupPass       string
	GUIPort          uint32
	TLSCertFile      string
	TLSKeyFile       string
	UseLEHTTPS       bool
	Domain           string
	ActiveNet        *chaincfg.Params
	BlockExplorerURL string
	Designation      string
	PoolFee          float64
	MinerPorts       map[string]uint32
}

// GUI represents the the mining pool user interface.
type GUI struct {
	cfg         *Config
	hub         *pool.Hub
	limiter     *pool.RateLimiter
	templates   *template.Template
	cookieStore *sessions.CookieStore
	router      *mux.Router
	server      *http.Server

	// The following fields cache pool data.
	workQuotas    []*pool.Quota
	workQuotasMtx sync.Mutex
	minedWork     []*pool.AcceptedWork
	minedWorkMtx  sync.Mutex
	poolHash      *big.Rat
	poolHashMtx   sync.Mutex
}

// generateSecret generates the CSRF secret.
func (ui *GUI) generateSecret() ([]byte, error) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	if err != nil {
		return nil, err
	}

	return secret, nil
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

	ui.router.HandleFunc("/", ui.GetIndex).Methods("GET")
	ui.router.HandleFunc("/admin", ui.GetAdmin).Methods("GET")
	ui.router.HandleFunc("/admin", ui.PostAdmin).Methods("POST")
	ui.router.HandleFunc("/backup", ui.PostBackup).Methods("POST")
	ui.router.HandleFunc("/logout", ui.PostLogout).Methods("POST")

	// Websocket endpoint allows the GUI to receive updated values
	ui.router.HandleFunc("/ws", ui.RegisterWebSocket).Methods("GET")
}

// renderTemplate executes the provided template.
func (ui *GUI) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
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
func NewGUI(cfg *Config, hub *pool.Hub, limiter *pool.RateLimiter) (*GUI, error) {
	ui := &GUI{
		cfg:        cfg,
		hub:        hub,
		limiter:    limiter,
		workQuotas: make([]*pool.Quota, 0),
		minedWork:  make([]*pool.AcceptedWork, 0),
		poolHash:   ZeroRat,
	}

	switch cfg.ActiveNet.Name {
	case chaincfg.TestNet3Params.Name:
		ui.cfg.BlockExplorerURL = "https://testnet.dcrdata.org"
	case chaincfg.SimNetParams.Name:
		ui.cfg.BlockExplorerURL = "..."
	default:
		ui.cfg.BlockExplorerURL = "https://explorer.dcrdata.org"
	}

	var err error
	ui.cfg.CSRFSecret, err = ui.hub.CSRFSecret(ui.generateSecret)
	if err != nil {
		return nil, err
	}

	ui.cookieStore = sessions.NewCookieStore(cfg.CSRFSecret)
	err = ui.loadTemplates()
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
			log.Tracef("Starting GUI server on port %d (https)", ui.cfg.GUIPort)
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
				log.Trace("Starting GUI server on port 80 (http, will forward to https)")
				if err := http.ListenAndServe(port80,
					certMgr.HTTPHandler(nil)); err != nil &&
					err != http.ErrServerClosed {
					log.Error(err)
				}
			}()

			log.Trace("Starting GUI server on port 443 (https)")
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
					work, err := ui.hub.FetchMinedWork()
					if err != nil {
						log.Error(err)
						continue
					}

					ui.minedWorkMtx.Lock()
					ui.minedWork = work
					ui.minedWorkMtx.Unlock()

					quotas, err := ui.hub.FetchWorkQuotas()
					if err != nil {
						log.Error(err)
						continue
					}

					ui.workQuotasMtx.Lock()
					ui.workQuotas = quotas
					ui.workQuotasMtx.Unlock()

					ui.poolHashMtx.Lock()
					ui.poolHash, _ = ui.hub.FetchPoolHashRate()
					ui.poolHashMtx.Unlock()

					ticks = 0
				}

				ui.updateWS()

			case <-ctx.Done():
				return
			}
		}
	}(ctx)
}
