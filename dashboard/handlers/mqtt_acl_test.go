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

// fakeMQTTACLBackend serves ACL rules; user methods unimplemented (returns
// ErrUnsupported) to keep this test fixture small.
type fakeMQTTACLBackend struct {
	mu     sync.Mutex
	rules  map[string]mqttauth.ACLRule
	nextID int
}

func newFakeACL() *fakeMQTTACLBackend {
	return &fakeMQTTACLBackend{rules: map[string]mqttauth.ACLRule{}}
}

func (f *fakeMQTTACLBackend) Kind() string                { return "fake" }
func (f *fakeMQTTACLBackend) Mode() mqttauth.AuthMode     { return mqttauth.ModeUsername }
func (f *fakeMQTTACLBackend) HashType() mqttauth.HashType { return mqttauth.HashNone }
func (f *fakeMQTTACLBackend) Close() error                { return nil }

func (f *fakeMQTTACLBackend) Users(context.Context) ([]mqttauth.User, error) {
	return nil, mqttauth.ErrUnsupported
}
func (f *fakeMQTTACLBackend) GetUser(context.Context, string) (*mqttauth.User, error) {
	return nil, mqttauth.ErrUnsupported
}
func (f *fakeMQTTACLBackend) PutUser(context.Context, mqttauth.User, string) error {
	return mqttauth.ErrUnsupported
}
func (f *fakeMQTTACLBackend) DeleteUser(context.Context, string) error {
	return mqttauth.ErrUnsupported
}

func (f *fakeMQTTACLBackend) Rules(_ context.Context, subject string) ([]mqttauth.ACLRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]mqttauth.ACLRule, 0, len(f.rules))
	for _, r := range f.rules {
		if subject == "" || r.Subject == subject {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeMQTTACLBackend) PutRule(_ context.Context, r mqttauth.ACLRule) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ID == "" {
		f.nextID++
		r.ID = "id-" + string(rune('0'+f.nextID))
	}
	f.rules[r.ID] = r
	return r.ID, nil
}

func (f *fakeMQTTACLBackend) DeleteRule(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rules[id]; !ok {
		return mqttauth.ErrNotFound
	}
	delete(f.rules, id)
	return nil
}

func newACLRenderer(t *testing.T) *Renderer {
	t.Helper()
	fs := fstest.MapFS{
		"templates/_layout.html":  &fstest.MapFile{Data: []byte(`{{define "layout"}}<html><body>{{template "nav" .}}{{template "content" .}}</body></html>{{end}}`)},
		"templates/_nav.html":     &fstest.MapFile{Data: []byte(`{{define "nav"}}<nav></nav>{{end}}`)},
		"templates/_flash.html":   &fstest.MapFile{Data: []byte(`{{define "flash"}}{{end}}`)},
		"templates/mqtt_acl.html": &fstest.MapFile{Data: []byte(`{{define "mqtt_acl"}}{{template "layout" .}}{{end}}{{define "content"}}KIND={{.BackendKind}} FILTER={{.SubjectFilter}} ITEMS={{len .Items}}{{range .Items}}|{{.Subject}}:{{.Topic}}={{.Access}}{{end}}{{end}}`)},
	}
	return NewRenderer(fs)
}

func TestMQTTACLList_AllAndFilter(t *testing.T) {
	be := newFakeACL()
	_, _ = be.PutRule(context.Background(), mqttauth.ACLRule{Subject: "alice", Topic: "x", Access: mqttauth.AccessRead})
	_, _ = be.PutRule(context.Background(), mqttauth.ACLRule{Subject: "alice", Topic: "y", Access: mqttauth.AccessWrite})
	_, _ = be.PutRule(context.Background(), mqttauth.ACLRule{Subject: "bob", Topic: "#", Access: mqttauth.AccessReadWrite})

	d := MQTTACLDeps{Backend: be, Renderer: newACLRenderer(t)}

	// All rules.
	req := adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-acl", nil))
	w := httptest.NewRecorder()
	MQTTACLList(d)(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "ITEMS=3") {
		t.Errorf("all-rules render wrong: code=%d body=%s", w.Code, w.Body.String())
	}

	// Filtered by alice.
	req = adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-acl?subject=alice", nil))
	w = httptest.NewRecorder()
	MQTTACLList(d)(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "ITEMS=2") || !strings.Contains(body, "FILTER=alice") {
		t.Errorf("filter render wrong: %s", body)
	}
}

func TestMQTTACLList_NotConfiguredWhenBackendNil(t *testing.T) {
	d := MQTTACLDeps{Backend: nil, Renderer: newACLRenderer(t)}
	req := adminCtx(httptest.NewRequest("GET", "/dashboard/mqtt-acl", nil))
	w := httptest.NewRecorder()
	MQTTACLList(d)(w, req)
	if w.Code != 200 {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMQTTACLCreate_HappyPath(t *testing.T) {
	be := newFakeACL()
	d := MQTTACLDeps{Backend: be, Renderer: newACLRenderer(t)}

	form := url.Values{"subject": {"alice"}, "topic": {"sensors/+/temp"}, "access": {"1"}}
	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-acl", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	MQTTACLCreate(d)(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	rules, _ := be.Rules(context.Background(), "alice")
	if len(rules) != 1 || rules[0].Topic != "sensors/+/temp" || rules[0].Access != mqttauth.AccessRead {
		t.Errorf("backend rule wrong: %+v", rules)
	}
}

func TestMQTTACLCreate_RejectsInvalidAccess(t *testing.T) {
	be := newFakeACL()
	d := MQTTACLDeps{Backend: be, Renderer: newACLRenderer(t)}

	form := url.Values{"subject": {"alice"}, "topic": {"x"}, "access": {"42"}}
	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-acl", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	MQTTACLCreate(d)(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 (re-render with error), got %d", w.Code)
	}
	rules, _ := be.Rules(context.Background(), "alice")
	if len(rules) != 0 {
		t.Errorf("backend should not have been called: %+v", rules)
	}
}

func TestMQTTACLDelete_Happy(t *testing.T) {
	be := newFakeACL()
	id, _ := be.PutRule(context.Background(), mqttauth.ACLRule{Subject: "alice", Topic: "x", Access: mqttauth.AccessRead})
	d := MQTTACLDeps{Backend: be, Renderer: newACLRenderer(t)}

	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-acl/"+id+"/delete", nil))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	MQTTACLDelete(d)(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	rules, _ := be.Rules(context.Background(), "")
	if len(rules) != 0 {
		t.Errorf("rule not deleted: %+v", rules)
	}
}

func TestMQTTACLDelete_MissingReturns404(t *testing.T) {
	be := newFakeACL()
	d := MQTTACLDeps{Backend: be, Renderer: newACLRenderer(t)}

	req := adminCtx(httptest.NewRequest("POST", "/dashboard/mqtt-acl/ghost/delete", nil))
	req.SetPathValue("id", "ghost")
	w := httptest.NewRecorder()
	MQTTACLDelete(d)(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", w.Code)
	}
}
