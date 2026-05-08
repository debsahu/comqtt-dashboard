// Package rest exposes dashboard-specific REST endpoints. They are layered
// on top of the comqtt broker's stock /api/v1/* surface (defined in
// github.com/wind-c/comqtt/v2/mqtt/rest), not a replacement for it.
//
// Wiring pattern (see cmd/comqtt-dashboard/main.go):
//
//	upstream := upstreamrest.New(server).GenHandlers()
//	addon    := rest.New(server).GenHandlers()
//	for k, v := range addon { upstream[k] = v }
//	dash, cleanup, _ := dashboard.Routes(...)
//	for k, v := range dash { upstream[k] = v }
//	listener := listeners.NewHTTP("stats", addr, nil, upstream)
package rest

import (
	"net/http"

	"github.com/wind-c/comqtt/v2/mqtt"
)

// Handler is the function signature shared with the upstream REST router so
// addon and upstream maps can be merged without conversion.
type Handler = func(http.ResponseWriter, *http.Request)

// Path constants for the dashboard-specific endpoints.
const (
	MqttListClientsPath       = "/api/v1/mqtt/clients"
	MqttUnsubscribeClientPath = "/api/v1/mqtt/clients/{id}/subscriptions/{topic}"
	MqttListSubscriptionsPath = "/api/v1/mqtt/subscriptions"
	MqttTopicsTreePath        = "/api/v1/mqtt/topics"
	MqttListRetainedPath      = "/api/v1/mqtt/retained"
	MqttClearRetainedPath     = "/api/v1/mqtt/retained/{topic}"
	MqttListSessionsPath      = "/api/v1/mqtt/sessions"
	MqttClearSessionPath      = "/api/v1/mqtt/sessions/{id}"
)

// Rest holds the broker reference for handler methods.
type Rest struct {
	server *mqtt.Server
}

// New returns a Rest bound to server.
func New(server *mqtt.Server) *Rest {
	return &Rest{server: server}
}

// GenHandlers returns the route map for the addon endpoints. Keys use the
// Go 1.22+ `METHOD /path` mux pattern.
func (s *Rest) GenHandlers() map[string]Handler {
	return map[string]Handler{
		"GET " + MqttListClientsPath:          s.listClients,
		"DELETE " + MqttUnsubscribeClientPath: s.unsubscribeClient,
		"GET " + MqttListSubscriptionsPath:    s.listSubscriptions,
		"GET " + MqttTopicsTreePath:           s.topicsTree,
		"GET " + MqttListRetainedPath:         s.listRetained,
		"DELETE " + MqttClearRetainedPath:     s.clearRetained,
		"GET " + MqttListSessionsPath:         s.listSessions,
		"DELETE " + MqttClearSessionPath:      s.clearSession,
	}
}
