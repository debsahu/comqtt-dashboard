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

	"github.com/debsahu/comqtt-dashboard/mqttauth"
)

// fakeMQTTBackend is a goroutine-safe in-memory mqttauth.Backend for handler
// tests. Records the last PutUser call so assertions can check the handler
// passed the right values through.
type fakeMQTTBackend struct {
	mu        sync.Mutex
	users     map[string]mqttauth.User
	lastPut   mqttauth.User
	lastPlain string
	kind      string
}

func newFakeMQTT() *fakeMQTTBackend {
	return &fakeMQTTBackend{users: map[string]mqttauth.User{}, kind: "fake"}
}

// newFakeMQTTWithKind returns a fakeMQTTBackend whose Kind() reports the
// given string. Used by banner-rendering tests that care about the
// BackendKind field set in mqttAuthPageData.
func newFakeMQTTWithKind(kind string) *fakeMQTTBackend {
	be := newFakeMQTT()
	be.kind = kind
	return be
}

func (f *fakeMQTTBackend) Kind() string                { return f.kind }
func (f *fakeMQTTBackend) Mode() mqttauth.AuthMode     { return mqttauth.ModeUsername }
func (f *fakeMQTTBackend) HashType() mqttauth.HashType { return mqttauth.HashBcrypt }
func (f *fakeMQTTBackend) Close() error                { return nil }

func (f *fakeMQTTBackend) Users(context.Context) ([]mqttauth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]mqttauth.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeMQTTBackend) GetUser(_ context.Context, subject string) (*mqttauth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[subject]
	if !ok {
		return nil, mqttauth.ErrNotFound
	}
	return &u, nil
}

func (f *fakeMQTTBackend) PutUser(_ context.Context, u mqttauth.User, plain string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.Subject] = u
	f.lastPut = u
	f.lastPlain = plain
	return nil
}

func (f *fakeMQTTBackend) DeleteUser(_ context.Context, subject string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.users[subject]; !ok {
		return mqttauth.ErrNotFound
	}
	delete(f.users, subject)
	return nil
}

func (f *fakeMQTTBackend) Rules(context.Context, string) ([]mqttauth.ACLRule, error) {
	return nil, nil
}
func (f *fakeMQTTBackend) PutRule(context.Context, mqttauth.ACLRule) (string, error) {
	return "", nil
}
func (f *fakeMQTTBackend) DeleteRule(context.Context, string) error { return nil }

// newAuthRenderer mounts the minimum templates needed to render mqtt_auth.
func newAuthRenderer(t *testing.T) *Renderer {
	t.Helper()
	fs := fstest.MapFS{
		"templates/_layout.html":   &fstest.MapFile{Data: []byte(`{{define "layout"}}<html><body>{{template "nav" .}}{{template "content" .}}</body></html>{{end}}`)},
		"templates/_nav.html":      &fstest.MapFile{Data: []byte(`{{define "nav"}}<nav></nav>{{end}}`)},
		"templates/_flash.html":    &fstest.MapFile{Data: []byte(`{{define "flash"}}{{end}}`)},
		"templates/mqtt_auth.html": &fstest.MapFile{Data: []byte(`{{define "mqtt_auth"}}{{template "layout" .}}{{end}}{{define "content"}}KIND={{.BackendKind}}{{if eq .BackendKind "file"}}|BANNER:file-restart{{end}} ITEMS={{len .Items}}{{range .Items}}|{{.Subject}}:{{.Allow}}{{end}}{{end}}`)},
	}
	return NewRenderer(fs)
}

func TestMQTTAuthList_RendersUsersFromBackend(t *testing.T) {
	be := newFakeMQTT()
	_ = be.PutUser(context.Background(), mqttauth.User{Subject: "alice", Allow: true}, "p")
	_ = be.PutUser(context.Background(), mqttauth.User{Subject: "bob", Allow: false}, "p")

	d := MQTTAuthDeps{Backend: be, Renderer: newAuthRenderer(t)}
	req := adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-auth", nil))
	w := httptest.NewRecorder()
	MQTTAuthList(d)(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "KIND=fake") {
		t.Errorf("body missing backend kind: %s", body)
	}
	if !strings.Contains(body, "ITEMS=2") {
		t.Errorf("body missing item count: %s", body)
	}
}

func TestMQTTAuthList_NotConfiguredWhenBackendNil(t *testing.T) {
	d := MQTTAuthDeps{Backend: nil, Renderer: newAuthRenderer(t)}
	req := adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-auth", nil))
	w := httptest.NewRecorder()
	MQTTAuthList(d)(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "KIND=") {
		t.Errorf("body missing structure: %s", w.Body.String())
	}
}

func TestMQTTAuthCreate_CallsBackendPutUser(t *testing.T) {
	be := newFakeMQTT()
	d := MQTTAuthDeps{Backend: be, Renderer: newAuthRenderer(t)}

	form := url.Values{"subject": {"alice"}, "password": {"hunter2"}, "allow": {"on"}}
	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-auth", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	MQTTAuthCreate(d)(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status=%d want 302; body=%s", w.Code, w.Body.String())
	}
	if be.lastPut.Subject != "alice" || !be.lastPut.Allow || be.lastPlain != "hunter2" {
		t.Errorf("backend received wrong args: subject=%q allow=%v plain=%q",
			be.lastPut.Subject, be.lastPut.Allow, be.lastPlain)
	}
}

func TestMQTTAuthCreate_RejectsEmptyFields(t *testing.T) {
	be := newFakeMQTT()
	d := MQTTAuthDeps{Backend: be, Renderer: newAuthRenderer(t)}

	form := url.Values{"subject": {""}, "password": {"x"}}
	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-auth", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	MQTTAuthCreate(d)(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 (re-rendered with error), got %d", w.Code)
	}
	if be.lastPut.Subject != "" {
		t.Errorf("backend should not have been called: %+v", be.lastPut)
	}
}

func TestMQTTAuthToggle_FlipsAllow(t *testing.T) {
	be := newFakeMQTT()
	_ = be.PutUser(context.Background(), mqttauth.User{Subject: "alice", Allow: true}, "p")
	d := MQTTAuthDeps{Backend: be, Renderer: newAuthRenderer(t)}

	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-auth/alice/toggle", nil))
	req.SetPathValue("subject", "alice")
	w := httptest.NewRecorder()
	MQTTAuthToggle(d)(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := be.GetUser(context.Background(), "alice")
	if got.Allow {
		t.Errorf("Allow should have been flipped to false")
	}
}

func TestMQTTAuthDelete_RemovesUser(t *testing.T) {
	be := newFakeMQTT()
	_ = be.PutUser(context.Background(), mqttauth.User{Subject: "alice", Allow: true}, "p")
	d := MQTTAuthDeps{Backend: be, Renderer: newAuthRenderer(t)}

	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-auth/alice/delete", nil))
	req.SetPathValue("subject", "alice")
	w := httptest.NewRecorder()
	MQTTAuthDelete(d)(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := be.GetUser(context.Background(), "alice"); err == nil {
		t.Errorf("user should have been deleted")
	}
}

func TestMQTTAuthList_FileBackendShowsBanner(t *testing.T) {
	be := newFakeMQTTWithKind("file")

	d := MQTTAuthDeps{Backend: be, Renderer: newAuthRenderer(t)}
	req := adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-auth", nil))
	w := httptest.NewRecorder()
	MQTTAuthList(d)(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "BANNER:file-restart") {
		t.Errorf("file backend should render restart banner; body=%s", body)
	}
}

func TestMQTTAuthList_NonFileBackendOmitsBanner(t *testing.T) {
	be := newFakeMQTTWithKind("redis")

	d := MQTTAuthDeps{Backend: be, Renderer: newAuthRenderer(t)}
	req := adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-auth", nil))
	w := httptest.NewRecorder()
	MQTTAuthList(d)(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "BANNER:file-restart") {
		t.Errorf("non-file backend should not render restart banner; body=%s", body)
	}
}
