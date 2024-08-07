// Copyright (c) 2020-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"context"
	"net/http"
	"strings"

	"github.com/decred/dcrpool/internal/pool"
	"github.com/gorilla/sessions"
)

const (
	// sessionKey is the key used to retrieve the current session from a request
	// context.
	sessionKey ctxKey = iota
)

type ctxKey int

// sessionMiddleware retrieves a session for the current request and adds it to
// the request context so it can be used by downstream middlewares/handlers.
func (ui *GUI) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := ui.cookieStore.Get(r, "session")
		if err != nil {
			// "value is not valid" occurs if the CSRF secret changes. This is
			// common during development (eg. when using the test harness) but
			// it should not occur in production.
			if strings.Contains(err.Error(), "securecookie: the value is not valid") {
				log.Warn("getSession error: CSRF secret has changed. Generating new session.")

				// Persist the generated session.
				err = ui.cookieStore.Save(r, w, session)
				if err != nil {
					log.Errorf("saveSession error: %v", err)
					http.Error(w, "Session error", http.StatusInternalServerError)
					return
				}
			} else {
				log.Errorf("getSession error: %v", err)
				http.Error(w, "Session error", http.StatusInternalServerError)
				return
			}
		}

		ctx := context.WithValue(r.Context(), sessionKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rateLimitMiddleware returns a "429 Too Many Requests" response if the client
// has exceeded its allowed limit, otherwise passes the request to the next
// middleware/handler.
func (ui *GUI) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Context().Value(sessionKey).(*sessions.Session)

		if !ui.cfg.WithinLimit(session.ID, pool.GUIClient) {
			http.Error(w, "Request limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
