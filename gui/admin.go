package gui

import (
	"html/template"
	"net/http"
	"strconv"

	bolt "github.com/coreos/bbolt"
	"github.com/gorilla/csrf"

	"github.com/decred/dcrpool/pool"
)

type adminPageData struct {
	Connections map[string][]*pool.ClientInfo
	CSRF        template.HTML
	Designation string
}

func (ui *GUI) GetAdmin(w http.ResponseWriter, r *http.Request) {
	pageData := adminPageData{
		CSRF:        csrf.TemplateField(r),
		Designation: ui.cfg.Designation,
	}

	session, _ := ui.cookieStore.Get(r, "session")
	if session.Values["IsAdmin"] != true {
		ui.renderTemplate(w, r, "login", pageData)
		return
	}

	pageData.Connections = ui.hub.FetchClientInfo()

	ui.renderTemplate(w, r, "admin", pageData)
}

func (ui *GUI) PostAdmin(w http.ResponseWriter, r *http.Request) {
	pass := r.FormValue("password")

	if ui.cfg.BackupPass != pass {
		log.Warn("Unauthorized access")
		ui.GetAdmin(w, r)
		return
	}

	session, _ := ui.cookieStore.Get(r, "session")
	session.Values["IsAdmin"] = true
	err := session.Save(r, w)
	if err != nil {
		log.Errorf("unable to save session: %v", err)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (ui *GUI) PostLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := ui.cookieStore.Get(r, "session")
	session.Values["IsAdmin"] = nil
	err := session.Save(r, w)
	if err != nil {
		log.Errorf("unable to save session: %v", err)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (ui *GUI) PostBackup(w http.ResponseWriter, r *http.Request) {
	session, _ := ui.cookieStore.Get(r, "session")
	if session.Values["IsAdmin"] != true {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	err := ui.db.View(func(tx *bolt.Tx) error {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="backup.db"`)
		w.Header().Set("Content-Length", strconv.Itoa(int(tx.Size())))
		_, err := tx.WriteTo(w)
		return err
	})

	if err != nil {
		log.Errorf("Error backing up database: %v", err)
		http.Error(w, "Error backing up database: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
