package webui

import (
	"bytes"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrpool/network"
	"github.com/gorilla/sessions"
)

var hub *network.Hub
var db *bolt.DB
var templates *template.Template
var store *sessions.CookieStore

func Init(h *network.Hub, d *bolt.DB, secret string, webUiDir string) error {
	hub = h
	db = d
	err := loadTemplates(filepath.Join(webUiDir, "templates"))
	if err != nil {
		return err
	}
	store = sessions.NewCookieStore([]byte(secret))
	return nil
}

func renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	var doc bytes.Buffer
	err := templates.ExecuteTemplate(&doc, name, data)
	if err != nil {
		log.Errorf("Template error: %v", err)
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	} else {
		doc.WriteTo(w)
	}
}

func loadTemplates(templatePath string) error {
	var templateFilename []string

	fn := func(path string, f os.FileInfo, err error) error {
		// If path doesn't exist, or other error with path, return error so that
		// Walk will quit and return the error to the caller.
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

	templates = template.Must(httpTemplates, nil)
	return nil
}
