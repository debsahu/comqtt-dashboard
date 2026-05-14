package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/debsahu/comqttauth"
)

// fakeRegexBackend implements comqttauth.Backend with no-op v0.1.0 methods
// (only the regex methods are exercised by the regex page handlers).
type fakeRegexBackend struct {
	mu    sync.Mutex
	rules map[string]comqttauth.RegexRule
}

func newFakeRegexBackend() *fakeRegexBackend {
	return &fakeRegexBackend{rules: map[string]comqttauth.RegexRule{}}
}

func (f *fakeRegexBackend) Kind() string                  { return "fake" }
func (f *fakeRegexBackend) Mode() comqttauth.AuthMode     { return comqttauth.ModeUsername }
func (f *fakeRegexBackend) HashType() comqttauth.HashType { return comqttauth.HashNone }
func (f *fakeRegexBackend) Close() error                  { return nil }

func (f *fakeRegexBackend) Users(context.Context) ([]comqttauth.User, error) {
	return nil, comqttauth.ErrUnsupported
}
func (f *fakeRegexBackend) GetUser(context.Context, string) (*comqttauth.User, error) {
	return nil, comqttauth.ErrUnsupported
}
func (f *fakeRegexBackend) PutUser(context.Context, comqttauth.User, string) error {
	return comqttauth.ErrUnsupported
}
func (f *fakeRegexBackend) DeleteUser(context.Context, string) error {
	return comqttauth.ErrUnsupported
}
func (f *fakeRegexBackend) Rules(context.Context, string) ([]comqttauth.ACLRule, error) {
	return nil, comqttauth.ErrUnsupported
}
func (f *fakeRegexBackend) PutRule(context.Context, comqttauth.ACLRule) (string, error) {
	return "", comqttauth.ErrUnsupported
}
func (f *fakeRegexBackend) DeleteRule(context.Context, string) error {
	return comqttauth.ErrUnsupported
}

func (f *fakeRegexBackend) RegexRules(context.Context) ([]comqttauth.RegexRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]comqttauth.RegexRule, 0, len(f.rules))
	for _, r := range f.rules {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRegexBackend) PutRegexRule(_ context.Context, r comqttauth.RegexRule) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ID == "" {
		r.ID = "r-test-" + r.SubjectPattern
	}
	f.rules[r.ID] = r
	return r.ID, nil
}

func (f *fakeRegexBackend) DeleteRegexRule(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rules[id]; !ok {
		return comqttauth.ErrNotFound
	}
	delete(f.rules, id)
	return nil
}

func (f *fakeRegexBackend) GetRegexSeeded(context.Context) (bool, error) { return false, nil }
func (f *fakeRegexBackend) SetRegexSeeded(context.Context) error         { return nil }

// newRegexRenderer mounts the minimum templates needed.
func newRegexRenderer(t *testing.T) *Renderer {
	t.Helper()
	fs := fstest.MapFS{
		"templates/_layout.html":        &fstest.MapFile{Data: []byte(`{{define "layout"}}<html><body>{{template "nav" .}}{{template "content" .}}</body></html>{{end}}`)},
		"templates/_nav.html":           &fstest.MapFile{Data: []byte(`{{define "nav"}}<nav></nav>{{end}}`)},
		"templates/_flash.html":         &fstest.MapFile{Data: []byte(`{{define "flash"}}{{end}}`)},
		"templates/mqtt_acl_regex.html": &fstest.MapFile{Data: []byte(`{{define "mqtt_acl_regex"}}{{template "layout" .}}{{end}}{{define "content"}}ITEMS={{len .Items}}{{range .Items}}|{{.ID}}:{{.Permission.String}}{{end}}{{end}}`)},
	}
	return NewRenderer(fs)
}

func TestMQTTACLRegexList_RendersRules(t *testing.T) {
	be := newFakeRegexBackend()
	_, _ = be.PutRegexRule(context.Background(), comqttauth.RegexRule{
		Order:         1,
		Permission:    comqttauth.PermissionAllow,
		SubjectKind:   comqttauth.SubjectUsername,
		Action:        comqttauth.ActionPub,
		TopicPatterns: []string{"x/#"},
	})

	d := MQTTACLRegexDeps{Backend: be, Renderer: newRegexRenderer(t), RegexEnabled: true}
	req := adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-acl-regex", nil))
	w := httptest.NewRecorder()
	MQTTACLRegexList(d)(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ITEMS=1") {
		t.Errorf("expected 1 item; got body=%s", w.Body.String())
	}
}

func TestMQTTACLRegexCreate_AcceptsValidForm(t *testing.T) {
	be := newFakeRegexBackend()
	d := MQTTACLRegexDeps{Backend: be, Renderer: newRegexRenderer(t), RegexEnabled: true}

	form := url.Values{}
	form.Set("order", "100")
	form.Set("permission", "allow")
	form.Set("subject_kind", "username")
	form.Set("subject_pattern", "alice")
	form.Set("action", "pub")
	form.Set("topic_patterns", "sensors/#") // textarea content

	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-acl-regex", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	MQTTACLRegexCreate(d)(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	rules, _ := be.RegexRules(context.Background())
	if len(rules) != 1 {
		t.Errorf("expected 1 rule; got %d", len(rules))
	}
}

func TestMQTTACLRegexCreate_RejectsMissingTopicPattern(t *testing.T) {
	be := newFakeRegexBackend()
	d := MQTTACLRegexDeps{Backend: be, Renderer: newRegexRenderer(t), RegexEnabled: true}

	form := url.Values{}
	form.Set("order", "100")
	form.Set("permission", "allow")
	form.Set("subject_kind", "username")
	form.Set("action", "pub")
	// topic_patterns omitted

	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-acl-regex", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	MQTTACLRegexCreate(d)(w, req)

	if w.Code == http.StatusFound {
		t.Errorf("expected non-redirect with error message; got redirect")
	}
	rules, _ := be.RegexRules(context.Background())
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after bad form; got %d", len(rules))
	}
}
