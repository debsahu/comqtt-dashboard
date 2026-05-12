package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBroadcastDelete_AnyPeerSucceedsReturns204(t *testing.T) {
	hit204 := newPeer(t, "/delete-me", 204, nil)
	miss := newPeer(t, "/delete-me", 404, nil)
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": hit204.URL, "node-B": miss.URL,
	})

	w := httptest.NewRecorder()
	r.broadcastDelete(w, "/delete-me")
	if w.Code != 204 {
		t.Fatalf("status=%d want 204", w.Code)
	}
}

func TestBroadcastDelete_AllPeers404Returns404(t *testing.T) {
	a := newPeer(t, "/delete-me", 404, nil)
	b := newPeer(t, "/delete-me", 404, nil)
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": a.URL, "node-B": b.URL,
	})

	w := httptest.NewRecorder()
	r.broadcastDelete(w, "/delete-me")
	if w.Code != 404 {
		t.Fatalf("status=%d want 404", w.Code)
	}
}

func TestBroadcastDelete_AllPeersErrorReturns502(t *testing.T) {
	a := newPeer(t, "/delete-me", 500, nil)
	b := newPeer(t, "/delete-me", 500, nil)
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": a.URL, "node-B": b.URL,
	})

	w := httptest.NewRecorder()
	r.broadcastDelete(w, "/delete-me")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d want 502", w.Code)
	}
}
