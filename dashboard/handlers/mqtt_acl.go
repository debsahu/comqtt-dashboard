// dashboard/handlers/mqtt_acl.go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/debsahu/comqtt-dashboard/dashboard/auth"
	"github.com/debsahu/comqtt-dashboard/mqttauth"
)

// MQTTACLDeps bundles dependencies for the Authorization (ACL CRUD) page.
// Backend may be nil; the page then renders a "not configured" notice.
type MQTTACLDeps struct {
	Backend  mqttauth.Backend
	Renderer *Renderer
	Cluster  bool
}

type mqttACLPageData struct {
	Title         string
	User          auth.User
	CSRF          string
	Cluster       bool
	Flash         string
	Error         string
	Configured    bool
	BackendKind   string
	ACLMode       string
	SubjectFilter string
	Items         []mqttACLRow
}

type mqttACLRow struct {
	ID      string
	Subject string
	Topic   string
	Access  string // deny | subscribe | publish | pubsub
	AccessN int    // numeric value, for the access edit dropdown
}

// MQTTACLList handles GET /dashboard/mqtt-acl. Optional ?subject= filter
// scopes the listing to a single user/clientid.
func MQTTACLList(d MQTTACLDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderMQTTACLPage(d, w, r, "", "")
	}
}

// MQTTACLCreate handles POST /dashboard/mqtt-acl.
func MQTTACLCreate(d MQTTACLDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Backend == nil {
			renderMQTTACLPage(d, w, r, "", "Backend not configured.")
			return
		}
		_ = r.ParseForm()
		subject := strings.TrimSpace(r.PostFormValue("subject"))
		topic := strings.TrimSpace(r.PostFormValue("topic"))
		accessStr := r.PostFormValue("access")
		access, err := strconv.Atoi(accessStr)
		if err != nil || access < 0 || access > 3 {
			renderMQTTACLPage(d, w, r, "", "Invalid access value (must be 0-3).")
			return
		}
		if subject == "" || topic == "" {
			renderMQTTACLPage(d, w, r, "", "Subject and topic are required.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, err := d.Backend.PutRule(ctx, mqttauth.ACLRule{
			Subject: subject,
			Topic:   topic,
			Access:  mqttauth.Access(access),
		}); err != nil {
			renderMQTTACLPage(d, w, r, "", "Create failed: "+err.Error())
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-acl", http.StatusFound)
	}
}

// MQTTACLDelete handles POST /dashboard/mqtt-acl/{id}/delete.
func MQTTACLDelete(d MQTTACLDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Backend == nil {
			http.Error(w, "backend not configured", http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := d.Backend.DeleteRule(ctx, id); err != nil {
			if errors.Is(err, mqttauth.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-acl", http.StatusFound)
	}
}

func renderMQTTACLPage(d MQTTACLDeps, w http.ResponseWriter, r *http.Request, flash, errMsg string) {
	subjectFilter := strings.TrimSpace(r.URL.Query().Get("subject"))
	page := mqttACLPageData{
		Title:         "Authorization",
		User:          auth.UserFromContext(r.Context()),
		CSRF:          auth.NewCSRFToken(),
		Cluster:       d.Cluster,
		Flash:         flash,
		Error:         errMsg,
		SubjectFilter: subjectFilter,
	}
	if d.Backend != nil {
		page.Configured = true
		page.BackendKind = d.Backend.Kind()
		page.ACLMode = d.Backend.Mode().String()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		rules, err := d.Backend.Rules(ctx, subjectFilter)
		if err != nil {
			page.Error = "List failed: " + err.Error()
		} else {
			rows := make([]mqttACLRow, 0, len(rules))
			for _, ru := range rules {
				rows = append(rows, mqttACLRow{
					ID:      ru.ID,
					Subject: ru.Subject,
					Topic:   ru.Topic,
					Access:  ru.Access.String(),
					AccessN: int(ru.Access),
				})
			}
			page.Items = rows
		}
	}
	d.Renderer.Render(w, "mqtt_acl", page)
}
