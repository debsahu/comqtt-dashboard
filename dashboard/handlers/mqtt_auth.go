// dashboard/handlers/mqtt_auth.go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/debsahu/comqtt-dashboard/dashboard/auth"
	"github.com/debsahu/comqtt-dashboard/mqttauth"
)

// MQTTAuthDeps bundles dependencies for the Authentication (MQTT user CRUD)
// page. Backend may be nil when the broker is configured with anonymous
// auth; the page renders a "not configured" notice in that case rather
// than failing the request.
type MQTTAuthDeps struct {
	Backend  mqttauth.Backend
	Renderer *Renderer
	Cluster  bool
}

type mqttAuthPageData struct {
	Title       string
	User        auth.User
	CSRF        string
	Cluster     bool
	Flash       string
	Error       string
	Configured  bool
	BackendKind string
	Mode        string
	HashType    string
	Items       []mqttUserRow
}

type mqttUserRow struct {
	Subject string
	Allow   bool
}

// MQTTAuthList handles GET /dashboard/mqtt-auth.
func MQTTAuthList(d MQTTAuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderMQTTAuthPage(d, w, r, "", "")
	}
}

// MQTTAuthCreate handles POST /dashboard/mqtt-auth.
func MQTTAuthCreate(d MQTTAuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Backend == nil {
			renderMQTTAuthPage(d, w, r, "", "Backend not configured.")
			return
		}
		_ = r.ParseForm()
		subject := strings.TrimSpace(r.PostFormValue("subject"))
		password := r.PostFormValue("password")
		allow := r.PostFormValue("allow") == "on"
		if subject == "" || password == "" {
			renderMQTTAuthPage(d, w, r, "", "Subject and password are required.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := d.Backend.PutUser(ctx, mqttauth.User{Subject: subject, Allow: allow}, password); err != nil {
			renderMQTTAuthPage(d, w, r, "", "Create failed: "+err.Error())
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-auth", http.StatusFound)
	}
}

// MQTTAuthToggle handles POST /dashboard/mqtt-auth/{subject}/toggle. Flips
// the Allow flag without changing the stored password.
func MQTTAuthToggle(d MQTTAuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Backend == nil {
			http.Error(w, "backend not configured", http.StatusServiceUnavailable)
			return
		}
		subject := r.PathValue("subject")
		if subject == "" {
			http.Error(w, "subject required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		u, err := d.Backend.GetUser(ctx, subject)
		if err != nil {
			if errors.Is(err, mqttauth.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := d.Backend.PutUser(ctx, mqttauth.User{Subject: subject, Allow: !u.Allow}, ""); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-auth", http.StatusFound)
	}
}

// MQTTAuthDelete handles POST /dashboard/mqtt-auth/{subject}/delete.
func MQTTAuthDelete(d MQTTAuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Backend == nil {
			http.Error(w, "backend not configured", http.StatusServiceUnavailable)
			return
		}
		subject := r.PathValue("subject")
		if subject == "" {
			http.Error(w, "subject required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := d.Backend.DeleteUser(ctx, subject); err != nil {
			if errors.Is(err, mqttauth.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-auth", http.StatusFound)
	}
}

func renderMQTTAuthPage(d MQTTAuthDeps, w http.ResponseWriter, r *http.Request, flash, errMsg string) {
	page := mqttAuthPageData{
		Title:   "Authentication",
		User:    auth.UserFromContext(r.Context()),
		CSRF:    auth.NewCSRFToken(),
		Cluster: d.Cluster,
		Flash:   flash,
		Error:   errMsg,
	}
	if d.Backend != nil {
		page.Configured = true
		page.BackendKind = d.Backend.Kind()
		page.Mode = d.Backend.Mode().String()
		page.HashType = d.Backend.HashType().String()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		users, err := d.Backend.Users(ctx)
		if err != nil {
			page.Error = "List failed: " + err.Error()
		} else {
			rows := make([]mqttUserRow, 0, len(users))
			for _, u := range users {
				rows = append(rows, mqttUserRow{Subject: u.Subject, Allow: u.Allow})
			}
			page.Items = rows
		}
	}
	d.Renderer.Render(w, "mqtt_auth", page)
}
