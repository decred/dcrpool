package webui

import (
	"bytes"
	"fmt"
	"html/template"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	bolt "github.com/coreos/bbolt"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"

	"github.com/decred/dcrpool/network"
)

type WebUI struct {
	hub   *network.Hub
	db    *bolt.DB
	tmpl  *template.Template
	store *sessions.CookieStore
}

func (ui *WebUI) GetRouter(secret string, secureCSRF bool, webUIDir string) *mux.Router {
	router := mux.NewRouter()

	csrfMiddleware := csrf.Protect(
		[]byte(secret),
		csrf.Secure(secureCSRF))

	router.Use(csrfMiddleware)

	cssDir := http.Dir(filepath.Join(webUIDir, "public/css"))
	router.PathPrefix("/css/").Handler(http.StripPrefix("/css/", http.FileServer(cssDir)))

	imagesDir := http.Dir(filepath.Join(webUIDir, "public/images"))
	router.PathPrefix("/images/").Handler(http.StripPrefix("/images/", http.FileServer(imagesDir)))

	router.HandleFunc("/", ui.GetIndex).Methods("GET")
	router.HandleFunc("/admin", ui.GetAdmin).Methods("GET")
	router.HandleFunc("/admin", ui.PostAdmin).Methods("POST")
	router.HandleFunc("/backup", ui.PostBackup).Methods("POST")
	router.HandleFunc("/logout", ui.PostLogout).Methods("POST")

	return router
}

func (ui *WebUI) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	var doc bytes.Buffer
	err := ui.tmpl.ExecuteTemplate(&doc, name, data)
	if err != nil {
		log.Errorf("template error: %v", err)
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	} else {
		doc.WriteTo(w)
	}
}

func (ui *WebUI) loadTemplates(templatePath string) error {
	var templateFilename []string

	fn := func(path string, f os.FileInfo, err error) error {
		// If path doesn't exist, or other error with path, return error so
		// that Walk will quit and return the error to the caller.
		if err != nil {
			return err
		}
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".html") {
			templateFilename = append(templateFilename, path)
		}
		return nil
	}

	err := filepath.Walk(templatePath, fn)
	if err != nil {
		return err
	}

	httpTemplates := template.New("template").Funcs(template.FuncMap{
		"hashString": hashString,
	})

	// Since template.Must panics with non-nil error, it is much more
	// informative to pass the error to the caller to log it and exit
	// gracefully.
	httpTemplates, err = httpTemplates.ParseFiles(templateFilename...)
	if err != nil {
		return err
	}

	ui.tmpl = template.Must(httpTemplates, nil)
	return nil
}

// InitUI starts the user interface of the pool.
func InitUI(hub *network.Hub, d *bolt.DB, secret string, UIDir string) (*WebUI, error) {
	ui := &WebUI{
		hub:   hub,
		db:    d,
		store: sessions.NewCookieStore([]byte(secret)),
	}

	err := ui.loadTemplates(filepath.Join(UIDir, "templates"))
	if err != nil {
		return nil, err
	}

	return ui, nil
}

var (
	// zeroRat is the default value for a big.Rat.
	zeroRat = new(big.Rat).SetInt64(0)

	// kiloHash is 1 KH represented as a big.Rat.
	kiloHash = new(big.Rat).SetInt64(1000)

	// megaHash is 1MH represented as a big.Rat.
	megaHash = new(big.Rat).SetInt64(1000000)

	// gigaHash is 1GH represented as a big.Rat.
	gigaHash = new(big.Rat).SetInt64(1000000000)

	// teraHash is 1TH represented as a big.Rat.
	teraHash = new(big.Rat).SetInt64(1000000000000)

	// petaHash is 1PH represented as a big.Rat
	petaHash = new(big.Rat).SetInt64(1000000000000000)
)

// hashString formats the provided hashrate per the best-fit unit.
func hashString(hash *big.Rat) string {
	if hash.Cmp(zeroRat) == 0 {
		return "0 H/s"
	}

	if hash.Cmp(petaHash) > 0 {
		ph := new(big.Rat).Quo(hash, petaHash)
		return fmt.Sprintf("%v PH/s", ph.FloatString(2))
	}

	if hash.Cmp(teraHash) > 0 {
		th := new(big.Rat).Quo(hash, teraHash)
		return fmt.Sprintf("%v TH/s", th.FloatString(2))
	}

	if hash.Cmp(gigaHash) > 0 {
		gh := new(big.Rat).Quo(hash, gigaHash)
		return fmt.Sprintf("%v GH/s", gh.FloatString(2))
	}

	if hash.Cmp(megaHash) > 0 {
		mh := new(big.Rat).Quo(hash, megaHash)
		return fmt.Sprintf("%v MH/s", mh.FloatString(2))
	}

	if hash.Cmp(kiloHash) > 0 {
		kh := new(big.Rat).Quo(hash, kiloHash)
		return fmt.Sprintf("%v KH/s", kh.FloatString(2))
	}

	return "< 1KH/s"
}
