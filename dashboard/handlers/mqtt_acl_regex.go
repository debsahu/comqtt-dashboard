// dashboard/handlers/mqtt_acl_regex.go
package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/debsahu/comqtt-dashboard/dashboard/auth"
	"github.com/debsahu/comqttauth"
)

// MQTTACLRegexDeps bundles dependencies for the new regex authorization
// page (/dashboard/mqtt-acl-regex).
type MQTTACLRegexDeps struct {
	Backend      comqttauth.Backend
	Renderer     *Renderer
	Cluster      bool
	RegexEnabled bool // mirrors Options.RegexBackend != nil
}

type mqttACLRegexPageData struct {
	Title        string
	User         auth.User
	CSRF         string
	Cluster      bool
	RegexEnabled bool
	Flash        string
	Error        string
	Items        []comqttauth.RegexRule
}

// MQTTACLRegexList handles GET /dashboard/mqtt-acl-regex.
func MQTTACLRegexList(d MQTTACLRegexDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderMQTTACLRegexPage(d, w, r, "", "")
	}
}

// MQTTACLRegexCreate handles POST /dashboard/mqtt-acl-regex.
func MQTTACLRegexCreate(d MQTTACLRegexDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rule, err := parseRegexRuleForm(r)
		if err != nil {
			renderMQTTACLRegexPage(d, w, r, "", err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, err := d.Backend.PutRegexRule(ctx, rule); err != nil {
			renderMQTTACLRegexPage(d, w, r, "", "Create failed: "+err.Error())
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-acl-regex", http.StatusFound)
	}
}

// MQTTACLRegexUpdate handles POST /dashboard/mqtt-acl-regex/{id}.
func MQTTACLRegexUpdate(d MQTTACLRegexDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		rule, err := parseRegexRuleForm(r)
		if err != nil {
			renderMQTTACLRegexPage(d, w, r, "", err.Error())
			return
		}
		rule.ID = id
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, err := d.Backend.PutRegexRule(ctx, rule); err != nil {
			renderMQTTACLRegexPage(d, w, r, "", "Update failed: "+err.Error())
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-acl-regex", http.StatusFound)
	}
}

// MQTTACLRegexDelete handles POST /dashboard/mqtt-acl-regex/{id}/delete.
func MQTTACLRegexDelete(d MQTTACLRegexDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := d.Backend.DeleteRegexRule(ctx, id); err != nil {
			if errors.Is(err, comqttauth.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-acl-regex", http.StatusFound)
	}
}

// MQTTACLRegexReorder handles POST /dashboard/mqtt-acl-regex/{id}/{up|down}.
// Adjusts Order by ±10 and re-puts the rule.
func MQTTACLRegexReorder(d MQTTACLRegexDeps, direction string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		rules, err := d.Backend.RegexRules(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var target comqttauth.RegexRule
		found := false
		for _, ru := range rules {
			if ru.ID == id {
				target = ru
				found = true
				break
			}
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		switch direction {
		case "up":
			target.Order -= 10
		case "down":
			target.Order += 10
		default:
			http.Error(w, "unknown direction", http.StatusBadRequest)
			return
		}
		if _, err := d.Backend.PutRegexRule(ctx, target); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/dashboard/mqtt-acl-regex", http.StatusFound)
	}
}

func renderMQTTACLRegexPage(d MQTTACLRegexDeps, w http.ResponseWriter, r *http.Request, flash, errMsg string) {
	page := mqttACLRegexPageData{
		Title:        "Authorization (regex)",
		User:         auth.UserFromContext(r.Context()),
		CSRF:         auth.NewCSRFToken(),
		Cluster:      d.Cluster,
		RegexEnabled: d.RegexEnabled,
		Flash:        flash,
		Error:        errMsg,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rules, err := d.Backend.RegexRules(ctx)
	if err != nil {
		page.Error = "List failed: " + err.Error()
	} else {
		page.Items = rules
	}
	d.Renderer.Render(w, "mqtt_acl_regex", page)
}

// parseRegexRuleForm reads the create/edit form into a comqttauth.RegexRule.
// topic_patterns comes from a textarea (one pattern per line); the parser
// splits on newlines and drops blank entries.
func parseRegexRuleForm(r *http.Request) (comqttauth.RegexRule, error) {
	if err := r.ParseForm(); err != nil {
		return comqttauth.RegexRule{}, fmt.Errorf("invalid form: %w", err)
	}
	orderStr := strings.TrimSpace(r.PostFormValue("order"))
	order, err := strconv.Atoi(orderStr)
	if err != nil {
		return comqttauth.RegexRule{}, fmt.Errorf("order must be an integer")
	}
	perm, ok := comqttauth.ParsePermission(r.PostFormValue("permission"))
	if !ok {
		return comqttauth.RegexRule{}, fmt.Errorf("permission must be allow or deny")
	}
	kind, ok := comqttauth.ParseSubjectKind(r.PostFormValue("subject_kind"))
	if !ok {
		return comqttauth.RegexRule{}, fmt.Errorf("subject_kind must be one of username/clientid/ipaddr/cert.cn/cert.subject")
	}
	act, ok := comqttauth.ParseRuleAction(r.PostFormValue("action"))
	if !ok {
		return comqttauth.RegexRule{}, fmt.Errorf("action must be pub, sub, or all")
	}

	// topic_patterns: textarea (one per line); drop blank lines.
	patternsRaw := r.PostFormValue("topic_patterns")
	patterns := strings.Split(patternsRaw, "\n")
	cleaned := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return comqttauth.RegexRule{}, fmt.Errorf("at least one topic_pattern required")
	}

	return comqttauth.RegexRule{
		Order:          order,
		Permission:     perm,
		SubjectKind:    kind,
		SubjectPattern: strings.TrimSpace(r.PostFormValue("subject_pattern")),
		Action:         act,
		TopicPatterns:  cleaned,
	}, nil
}
