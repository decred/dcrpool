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
	"time"

	"golang.org/x/crypto/acme/autocert"

	bolt "github.com/coreos/bbolt"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrpool/pool"
)

var (
	// ZeroInt is the default value for a big.Int.
	ZeroInt = new(big.Int).SetInt64(0)

	// ZeroRat is the default value for a big.Rat.
	ZeroRat = new(big.Rat).SetInt64(0)

	// KiloHash is 1 KH represented as a big.Rat.
	KiloHash = new(big.Rat).SetInt64(1000)

	// MegaHash is 1MH represented as a big.Rat.
	MegaHash = new(big.Rat).SetInt64(1000000)

	// GigaHash is 1GH represented as a big.Rat.
	GigaHash = new(big.Rat).SetInt64(1000000000)

	// TeraHash is 1TH represented as a big.Rat.
	TeraHash = new(big.Rat).SetInt64(1000000000000)

	// PetaHash is 1PH represented as a big.Rat
	PetaHash = new(big.Rat).SetInt64(1000000000000000)
)

// HashString formats the provided hashrate per the best-fit unit.
func hashString(hash *big.Rat) string {
	if hash.Cmp(ZeroRat) == 0 {
		return "0 H/s"
	}

	if hash.Cmp(PetaHash) > 0 {
		ph := new(big.Rat).Quo(hash, PetaHash)
		return fmt.Sprintf("%v PH/s", ph.FloatString(4))
	}

	if hash.Cmp(TeraHash) > 0 {
		th := new(big.Rat).Quo(hash, TeraHash)
		return fmt.Sprintf("%v TH/s", th.FloatString(4))
	}

	if hash.Cmp(GigaHash) > 0 {
		gh := new(big.Rat).Quo(hash, GigaHash)
		return fmt.Sprintf("%v GH/s", gh.FloatString(4))
	}

	if hash.Cmp(MegaHash) > 0 {
		mh := new(big.Rat).Quo(hash, MegaHash)
		return fmt.Sprintf("%v MH/s", mh.FloatString(4))
	}

	if hash.Cmp(KiloHash) > 0 {
		kh := new(big.Rat).Quo(hash, KiloHash)
		return fmt.Sprintf("%v KH/s", kh.FloatString(4))
	}

	return "< 1KH/s"
}

// formatUnixTime formats the provided integer as a UTC time string,
func formatUnixTime(unix int64) string {
	return time.Unix(0, unix).Format("2-Jan-2006 15:04:05 MST")
}

// floatToPercent formats the provided float64 as a percentage,
// rounded to the nearest decimal place. eg. "10.5%"
func floatToPercent(rat float64) string {
	rat = rat * 100
	str := fmt.Sprintf("%.1f", rat)
	return str + "%"
}

// ratToPercent formats the provided big.Rat as a percentage,
// rounded to the nearest decimal place. eg. "10.5%"
func ratToPercent(rat *big.Rat) string {
	real, _ := rat.Float64()
	return floatToPercent(real)
}

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
}

// GUI represents the the mining pool user interface.
type GUI struct {
	cfg         *Config
	hub         *pool.Hub
	db          *bolt.DB
	templates   *template.Template
	cookieStore *sessions.CookieStore
	router      *mux.Router
	server      *http.Server
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

	_, err = doc.WriteTo(w)
	if err != nil {
		log.Errorf("unable to render template: %v", err)
	}
}

// NewGUI creates an instance of the user interface.
func NewGUI(cfg *Config, hub *pool.Hub, db *bolt.DB) (*GUI, error) {
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
		"hashString":     hashString,
		"upper":          strings.ToUpper,
		"ratToPercent":   ratToPercent,
		"floatToPercent": floatToPercent,
		"time":           formatUnixTime,
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
