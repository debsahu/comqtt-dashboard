package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wind-c/comqtt/v2/cluster/discovery"
)

// fakeAgent satisfies peerLister with a static list of members.
type fakeAgent struct {
	local   string
	members []discovery.Member
}

func (a *fakeAgent) GetMemberList() []discovery.Member { return a.members }
func (a *fakeAgent) GetLocalName() string              { return a.local }

// newTestRest wires a Rest whose urlFor maps each member name to a base URL.
// Callers spin up httptest.Servers per node and pass {name: server.URL}.
// Member order in agent.GetMemberList matches the order names appear in the
// slice; pass an ordered slice if your test cares (most don't, but the
// listClients dedupe test does because "first wins" depends on order).
func newTestRest(t *testing.T, names []string, nodeURLs map[string]string) *Rest {
	t.Helper()
	members := make([]discovery.Member, len(names))
	for i, name := range names {
		members[i] = discovery.Member{Name: name, Addr: "ignored"}
	}
	return &Rest{
		agent: &fakeAgent{local: "local", members: members},
		urlFor: func(m discovery.Member, pq string) string {
			return nodeURLs[m.Name] + pq
		},
	}
}

// pageBody renders an addon-shape pagination envelope. Items must be a
// JSON-encodable slice.
func pageBody(t *testing.T, items any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"page":  1,
		"size":  500,
		"total": -1, // unused by the cluster aggregator
		"items": items,
	})
	if err != nil {
		t.Fatalf("pageBody: %v", err)
	}
	return b
}

// route registers a single-path responder on an httptest.Server. Convenience
// over building a full mux for one-path peers.
func newPeer(t *testing.T, path string, status int, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
