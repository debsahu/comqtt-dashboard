// Package rest mirrors the dashboard addon's per-node REST endpoints
// (github.com/debsahu/comqtt-dashboard/rest) as cluster-aggregating handlers.
// A request hitting any cluster node fans out to every member, merges the
// per-node responses, and returns a single unified payload.
//
// The aggregation behavior depends on endpoint shape:
//   - List endpoints (clients, subscriptions, retained, sessions): each peer
//     returns its local rest.Page[T] envelope; this package merges all
//     Items, then re-paginates the merged slice and returns a single Page[T].
//   - Topics tree: per-peer trees are merged into one tree.
//   - DELETE endpoints: broadcast to every node. Each peer handles the local
//     case (subscriber on that node, retained-cache slot on that node, etc.)
//     and 404s for state it doesn't own. The mirror returns 204 if any peer
//     succeeded, 404 if none did.
//
// Routes are mounted under /api/v1/cluster/mqtt/* to distinguish them from
// the per-node /api/v1/mqtt/* surface. The cluster-mode binary registers
// both maps on the same HTTP listener.
package rest

import (
	"net/http"
	"strings"

	cs "github.com/wind-c/comqtt/v2/cluster"
	"github.com/wind-c/comqtt/v2/cluster/discovery"
)

// Handler matches the upstream router signature so addon-cluster and upstream
// handler maps can be merged without conversion.
type Handler = func(http.ResponseWriter, *http.Request)

// Path constants for the cluster-mirror endpoints. Each mirrors one entry
// in github.com/debsahu/comqtt-dashboard/rest under /api/v1/cluster/.
const (
	ClusterMqttListClientsPath       = "/api/v1/cluster/mqtt/clients"
	ClusterMqttUnsubscribeClientPath = "/api/v1/cluster/mqtt/clients/{id}/subscriptions/{topic}"
	ClusterMqttListSubscriptionsPath = "/api/v1/cluster/mqtt/subscriptions"
	ClusterMqttTopicsTreePath        = "/api/v1/cluster/mqtt/topics"
	ClusterMqttListRetainedPath      = "/api/v1/cluster/mqtt/retained"
	ClusterMqttClearRetainedPath     = "/api/v1/cluster/mqtt/retained/{topic}"
	ClusterMqttListSessionsPath      = "/api/v1/cluster/mqtt/sessions"
	ClusterMqttClearSessionPath      = "/api/v1/cluster/mqtt/sessions/{id}"
)

// peerLister is the surface from *cluster.Agent that the fan-out helpers
// need. Real callers pass *cs.Agent; tests inject a fake.
type peerLister interface {
	GetMemberList() []discovery.Member
	GetLocalName() string
}

// Rest holds the cluster agent and the URL builder for cross-peer calls.
// Cluster discovery (serf) records each member's gossip address and tags
// grpc/raft ports, but not the dashboard HTTP port, so we assume every node
// listens on the same port the local node does (cfg.Mqtt.HTTP).
type Rest struct {
	agent peerLister
	// urlFor builds the full URL for calling pathAndQuery on member m. The
	// default form is "http://<m.Addr>:<httpPort><pathAndQuery>"; tests
	// inject a stub mapping members to httptest.Server URLs.
	urlFor func(m discovery.Member, pathAndQuery string) string
}

// New returns a Rest bound to agent. httpAddr is the local node's HTTP listen
// address (e.g. ":8080"); only the port portion is used to build peer URLs.
func New(agent *cs.Agent, httpAddr string) *Rest {
	port := portOf(httpAddr)
	return &Rest{
		agent: agent,
		urlFor: func(m discovery.Member, pathAndQuery string) string {
			return "http://" + m.Addr + ":" + port + pathAndQuery
		},
	}
}

// GenHandlers returns the route map for the cluster-mirror endpoints. Keys
// use the Go 1.22+ `METHOD /path/{params}` mux pattern.
func (s *Rest) GenHandlers() map[string]Handler {
	return map[string]Handler{
		"GET " + ClusterMqttListClientsPath:          s.listClients,
		"DELETE " + ClusterMqttUnsubscribeClientPath: s.unsubscribeClient,
		"GET " + ClusterMqttListSubscriptionsPath:    s.listSubscriptions,
		"GET " + ClusterMqttTopicsTreePath:           s.topicsTree,
		"GET " + ClusterMqttListRetainedPath:         s.listRetained,
		"DELETE " + ClusterMqttClearRetainedPath:     s.clearRetained,
		"GET " + ClusterMqttListSessionsPath:         s.listSessions,
		"DELETE " + ClusterMqttClearSessionPath:      s.clearSession,
	}
}

// peerURLs builds one URL per cluster member for the given addon REST path.
// The path is appended verbatim and may include a query string; callers are
// responsible for URL-encoding path variables.
func (s *Rest) peerURLs(pathAndQuery string) []string {
	ms := s.agent.GetMemberList()
	urls := make([]string, len(ms))
	for i, m := range ms {
		urls[i] = s.urlFor(m, pathAndQuery)
	}
	return urls
}

// portOf splits the port off a "[host]:port" or ":port" listen address. If
// addr is empty or malformed, returns "8080" as a sane default.
func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 && i < len(addr)-1 {
		return addr[i+1:]
	}
	return "8080"
}
