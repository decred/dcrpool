package homepage

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"github.com/decred/dcrpool/network"
)

var tmpl *template.Template

type homePageData struct {
	PageTitle         string
	HashRate          string
	Connections       map[string]uint32
	LastPaymentHeight string
	LastWorkHeight    uint32
	MinedWork         []*network.AcceptedWork
	WorkQuotas        map[string]interface{}
}

var hub *network.Hub

func Init(h *network.Hub) {
	tmpl = template.Must(template.ParseFiles("homepage/homepage.html"))
	hub = h
}

func Get(w http.ResponseWriter, r *http.Request) {
	workQuotas, err := hub.FetchWorkQuotas()
	if err != nil {
		http.Error(w, "FetchWorkQuotas: "+err.Error(), http.StatusInternalServerError)
		return
	}

	work, err := hub.FetchMinedWork()
	if err != nil {
		http.Error(w, "FetchMinedWork: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lastPaymentHeight, err := hub.FetchLastPaymentHeight()
	if err != nil {
		http.Error(w, "FetchLastPaymentHeight: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := homePageData{
		HashRate:          fmt.Sprintf("%v TH/s", hub.FetchHash().FloatString(12)),
		Connections:       hub.FetchConnections(),
		LastWorkHeight:    hub.FetchLastWorkHeight(),
		WorkQuotas:        workQuotas,
		MinedWork:         work,
		LastPaymentHeight: strconv.FormatUint(uint64(lastPaymentHeight), 10),
	}

	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, data)
	if err != nil {
		log.Errorf("GET / 500 - Error rendering template: %v", err)
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	} else {
		buf.WriteTo(w)
		log.Info("GET / 200")
	}
}
