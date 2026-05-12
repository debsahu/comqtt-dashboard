package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

func TestListClients_FanOutMergesAndLabelsNode(t *testing.T) {
	a := newPeer(t, addon.MqttListClientsPath, 200, pageBody(t, []clientWire{
		{ClientID: "alpha"}, {ClientID: "beta"},
	}))
	b := newPeer(t, addon.MqttListClientsPath, 200, pageBody(t, []clientWire{
		{ClientID: "gamma"}, {ClientID: "delta"},
	}))
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": a.URL, "node-B": b.URL,
	})

	req := httptest.NewRequest("GET", "/api/v1/cluster/mqtt/clients", nil)
	w := httptest.NewRecorder()
	r.listClients(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got addon.Page[clientWire]
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if got.Total != 4 {
		t.Fatalf("Total=%d want 4 (items=%v)", got.Total, got.Items)
	}
	// Items sorted by ClientID
	wantOrder := []string{"alpha", "beta", "delta", "gamma"}
	for i, want := range wantOrder {
		if got.Items[i].ClientID != want {
			t.Errorf("Items[%d].ClientID=%q want %q", i, got.Items[i].ClientID, want)
		}
	}
	// Node tag annotated per source
	nodeFor := map[string]string{}
	for _, it := range got.Items {
		nodeFor[it.ClientID] = it.Node
	}
	if nodeFor["alpha"] != "node-A" || nodeFor["delta"] != "node-B" {
		t.Errorf("Node tagging wrong: %v", nodeFor)
	}
}

func TestListClients_DedupesAcrossNodes(t *testing.T) {
	// Same client appears on both nodes (e.g. transient cluster split).
	// First-seen wins; the second copy is dropped.
	a := newPeer(t, addon.MqttListClientsPath, 200, pageBody(t, []clientWire{
		{ClientID: "shared", Username: "from-A"},
	}))
	b := newPeer(t, addon.MqttListClientsPath, 200, pageBody(t, []clientWire{
		{ClientID: "shared", Username: "from-B"},
	}))
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": a.URL, "node-B": b.URL,
	})

	req := httptest.NewRequest("GET", "/api/v1/cluster/mqtt/clients", nil)
	w := httptest.NewRecorder()
	r.listClients(w, req)

	var got addon.Page[clientWire]
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Total != 1 {
		t.Fatalf("Total=%d want 1 (dedup failed)", got.Total)
	}
	if got.Items[0].Username != "from-A" {
		t.Errorf("Dedupe should keep first-seen (node-A); got Username=%q", got.Items[0].Username)
	}
}

func TestListClients_PeerDownDoesNotFail(t *testing.T) {
	// One real peer, one returning 500 (simulating a half-up node).
	up := newPeer(t, addon.MqttListClientsPath, 200, pageBody(t, []clientWire{
		{ClientID: "up-only"},
	}))
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(down.Close)
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": up.URL, "node-B": down.URL,
	})

	req := httptest.NewRequest("GET", "/api/v1/cluster/mqtt/clients", nil)
	w := httptest.NewRecorder()
	r.listClients(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got addon.Page[clientWire]
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Total != 1 || got.Items[0].ClientID != "up-only" {
		t.Fatalf("want 1 item from up node, got Total=%d items=%v", got.Total, got.Items)
	}
}

func TestListClients_PaginationOnMergedSlice(t *testing.T) {
	// Each peer returns 5 items; with size=3, page=2 should return items 4-6
	// from the merged-sorted slice.
	items := func(prefix string) []clientWire {
		return []clientWire{
			{ClientID: prefix + "1"}, {ClientID: prefix + "2"}, {ClientID: prefix + "3"},
			{ClientID: prefix + "4"}, {ClientID: prefix + "5"},
		}
	}
	a := newPeer(t, addon.MqttListClientsPath, 200, pageBody(t, items("a")))
	b := newPeer(t, addon.MqttListClientsPath, 200, pageBody(t, items("b")))
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": a.URL, "node-B": b.URL,
	})

	req := httptest.NewRequest("GET", "/api/v1/cluster/mqtt/clients?page=2&size=3", nil)
	w := httptest.NewRecorder()
	r.listClients(w, req)

	var got addon.Page[clientWire]
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Total != 10 {
		t.Fatalf("Total=%d want 10", got.Total)
	}
	if got.Page != 2 || got.Size != 3 {
		t.Errorf("Page=%d Size=%d want 2/3", got.Page, got.Size)
	}
	if len(got.Items) != 3 {
		t.Fatalf("len(Items)=%d want 3", len(got.Items))
	}
	// Merged sort = a1,a2,a3,a4,a5,b1,b2,b3,b4,b5
	// page=2 size=3 -> items 4..6 = a4, a5, b1
	want := []string{"a4", "a5", "b1"}
	for i, w := range want {
		if got.Items[i].ClientID != w {
			t.Errorf("Items[%d].ClientID=%q want %q", i, got.Items[i].ClientID, w)
		}
	}
}
