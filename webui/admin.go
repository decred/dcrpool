package webui

import (
	"html/template"
	"net/http"
	"strconv"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrpool/network"
	"github.com/gorilla/csrf"
)

type adminPageData struct {
	Connections map[string][]*network.ClientInfo
	WorkQuotas  *network.WorkQuotas
	Admin       bool
	CSRF        template.HTML
}

func (ui *WebUI) GetAdmin(w http.ResponseWriter, r *http.Request) {
	session, _ := ui.store.Get(r, "session")
	if session.Values["IsAdmin"] != true {
		ui.renderTemplate(w, r, "admin", adminPageData{
			Admin: false,
			CSRF:  csrf.TemplateField(r),
		})
		return
	}

	workQuotas, err := ui.hub.FetchWorkQuotas()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchWorkQuotas error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ui.renderTemplate(w, r, "admin", adminPageData{
		Connections: ui.hub.FetchClientInfo(),
		WorkQuotas:  workQuotas,
		Admin:       true,
		CSRF:        csrf.TemplateField(r),
	})
}

func (ui *WebUI) PostAdmin(w http.ResponseWriter, r *http.Request) {
	pass := r.FormValue("password")

	if !ui.hub.CheckBackupPass(pass) {
		log.Warn("Admin password test failed")
		ui.GetAdmin(w, r)
		return
	}

	session, _ := ui.store.Get(r, "session")
	session.Values["IsAdmin"] = true
	session.Save(r, w)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (ui *WebUI) PostLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := ui.store.Get(r, "session")
	session.Values["IsAdmin"] = nil
	session.Save(r, w)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (ui *WebUI) PostBackup(w http.ResponseWriter, r *http.Request) {
	session, _ := ui.store.Get(r, "session")
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
