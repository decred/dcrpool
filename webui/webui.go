package webui

import (
	"bytes"
	"html/template"
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

	// Since template.Must panics with non-nil error, it is much more
	// informative to pass the error to the caller to log it and exit
	// gracefully.
	httpTemplates, err := template.ParseFiles(templateFilename...)
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
