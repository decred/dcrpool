package gui

import (
	"bytes"
	"context"
	"crypto/rand"
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

	bolt "github.com/coreos/bbolt"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrpool/database"
	"github.com/decred/dcrpool/network"
	"github.com/decred/dcrpool/util"
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
}

// GUI represents the the mining pool user interface.
type GUI struct {
	cfg         *Config
	hub         *network.Hub
	db          *bolt.DB
	templates   *template.Template
	cookieStore *sessions.CookieStore
	router      *mux.Router
	server      *http.Server
}

// GenerateSecret generates the CSRF secret.
func (ui *GUI) GenerateSecret() []byte {
	secret := make([]byte, 32)
	rand.Read(secret)
	return secret
}

// route configures the http router of the user interface.
func (ui *GUI) route() {
	ui.router = mux.NewRouter()
	ui.router.Use(csrf.Protect(ui.cfg.CSRFSecret, csrf.Secure(true)))

	cssDir := http.Dir(filepath.Join(ui.cfg.GUIDir, "public/css"))
	ui.router.PathPrefix("/css/").Handler(http.StripPrefix("/css/",
		http.FileServer(cssDir)))

	imagesDir := http.Dir(filepath.Join(ui.cfg.GUIDir, "public/images"))
	ui.router.PathPrefix("/images/").Handler(http.StripPrefix("/images/",
		http.FileServer(imagesDir)))

	ui.router.HandleFunc("/", ui.GetIndex).Methods("GET")
	ui.router.HandleFunc("/admin", ui.GetAdmin).Methods("GET")
	ui.router.HandleFunc("/admin", ui.PostAdmin).Methods("POST")
	ui.router.HandleFunc("/backup", ui.PostBackup).Methods("POST")
	ui.router.HandleFunc("/logout", ui.PostLogout).Methods("POST")
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

	doc.WriteTo(w)
}

// NewGUI creates an instance of the user interface.
func NewGUI(cfg *Config, hub *network.Hub, db *bolt.DB) (*GUI, error) {
	ui := &GUI{
		cfg: cfg,
		hub: hub,
		db:  db,
	}

	switch cfg.ActiveNet.Name {
	case chaincfg.TestNet3Params.Name:
		ui.cfg.BlockExplorerURL = "https://testnet.dcrdata.org"
	default:
		ui.cfg.BlockExplorerURL = "https://explorer.dcrdata.org"
	}

	// Fetch or generate the CSRF secret.
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)

		CSRFSecret := pbkt.Get(database.CSRFSecret)
		if CSRFSecret == nil {
			log.Info("CSRF secret value not found in db, initializing.")

			CSRFSecret = ui.GenerateSecret()
			err := pbkt.Put(database.CSRFSecret, CSRFSecret)
			if err != nil {
				return err
			}
		}

		if CSRFSecret != nil {
			ui.cfg.CSRFSecret = CSRFSecret
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	ui.cookieStore = sessions.NewCookieStore(cfg.CSRFSecret)
	ui.loadTemplates()
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
		"hashString":    util.HashString,
		"upper":         strings.ToUpper,
		"percentString": util.PercentString,
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
func (ui *GUI) Run() {
	go func() {
		if !ui.cfg.UseLEHTTPS {
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
				if err := http.ListenAndServe(port80,
					certMgr.HTTPHandler(nil)); err != nil &&
					err != http.ErrServerClosed {
					log.Error(err)
				}
			}()

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
}
