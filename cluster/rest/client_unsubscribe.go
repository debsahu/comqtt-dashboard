package rest

import (
	"net/http"
	"net/url"
	"strings"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

// unsubscribeClient handles DELETE /api/v1/cluster/mqtt/clients/{id}/subscriptions/{topic}.
//
// Broadcasts to every node; the node that holds the client connection
// removes the subscription and returns 204, the others 404. The mirror
// returns 204 if any peer succeeded.
func (s *Rest) unsubscribeClient(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	topic := r.PathValue("topic")
	if clientID == "" || topic == "" {
		http.Error(w, "missing client id or topic", http.StatusBadRequest)
		return
	}
	path := addon.MqttUnsubscribeClientPath
	path = strings.Replace(path, "{id}", url.PathEscape(clientID), 1)
	path = strings.Replace(path, "{topic}", url.PathEscape(topic), 1)
	s.broadcastDelete(w, path)
}
