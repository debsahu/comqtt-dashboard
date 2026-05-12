package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

func TestTopicsTree_MergesAndSumsSubscribers(t *testing.T) {
	// Both nodes have "sensors/temp"; counts should sum.
	// node-A also has "sensors/humidity"; node-B also has "alerts".
	treeA := &topicNode{Children: []*topicNode{
		{Topic: "sensors", Children: []*topicNode{
			{Topic: "temp", Subscribers: 3},
			{Topic: "humidity", Subscribers: 1},
		}},
	}}
	treeB := &topicNode{Children: []*topicNode{
		{Topic: "sensors", Children: []*topicNode{
			{Topic: "temp", Subscribers: 2},
		}},
		{Topic: "alerts", Subscribers: 5},
	}}
	bodyA, _ := json.Marshal(treeA)
	bodyB, _ := json.Marshal(treeB)

	a := newPeer(t, addon.MqttTopicsTreePath, 200, bodyA)
	b := newPeer(t, addon.MqttTopicsTreePath, 200, bodyB)
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": a.URL, "node-B": b.URL,
	})

	req := httptest.NewRequest("GET", "/api/v1/cluster/mqtt/topics", nil)
	w := httptest.NewRecorder()
	r.topicsTree(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got topicNode
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Tree sorted: alerts, sensors -> humidity, temp
	if len(got.Children) != 2 {
		t.Fatalf("root children=%d want 2", len(got.Children))
	}
	if got.Children[0].Topic != "alerts" || got.Children[0].Subscribers != 5 {
		t.Errorf("alerts: got %+v", got.Children[0])
	}
	sensors := got.Children[1]
	if sensors.Topic != "sensors" || len(sensors.Children) != 2 {
		t.Fatalf("sensors: got %+v", sensors)
	}
	if sensors.Children[0].Topic != "humidity" || sensors.Children[0].Subscribers != 1 {
		t.Errorf("humidity: got %+v", sensors.Children[0])
	}
	if sensors.Children[1].Topic != "temp" || sensors.Children[1].Subscribers != 5 {
		t.Errorf("temp subscribers should be 3+2=5, got %+v", sensors.Children[1])
	}
}

func TestTopicsTree_OnePeerDownStillReturnsOther(t *testing.T) {
	treeA := &topicNode{Children: []*topicNode{{Topic: "only-A", Subscribers: 1}}}
	bodyA, _ := json.Marshal(treeA)

	a := newPeer(t, addon.MqttTopicsTreePath, 200, bodyA)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	t.Cleanup(down.Close)
	r := newTestRest(t, []string{"node-A", "node-B"}, map[string]string{
		"node-A": a.URL, "node-B": down.URL,
	})

	req := httptest.NewRequest("GET", "/api/v1/cluster/mqtt/topics", nil)
	w := httptest.NewRecorder()
	r.topicsTree(w, req)

	var got topicNode
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Children) != 1 || got.Children[0].Topic != "only-A" {
		t.Errorf("want one child only-A, got %+v", got.Children)
	}
}
