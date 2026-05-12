package rest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

type sessionWire struct {
	ClientID              string `json:"client_id"`
	Username              string `json:"username"`
	Online                bool   `json:"online"`
	Remote                string `json:"remote"`
	Listener              string `json:"listener,omitempty"`
	ProtocolVersion       byte   `json:"protocol_version"`
	Clean                 bool   `json:"clean"`
	Subs                  int    `json:"subs"`
	Inflight              int    `json:"inflight"`
	SessionExpiryInterval uint32 `json:"session_expiry_interval,omitempty"`
	Node                  string `json:"node,omitempty"`
}

// listSessions handles GET /api/v1/cluster/mqtt/sessions.
//
// Fans out across the cluster. Online sessions are unique to their host
// node; offline (stored) sessions come from the shared storage backend so
// every node returns them - we dedupe by ClientID, preferring the online
// record when both online and offline exist (online wins because that's the
// live state). ?online=true|false filter is forwarded.
func (s *Rest) listSessions(w http.ResponseWriter, r *http.Request) {
	onlineQ := r.URL.Query().Get("online")
	members := s.agent.GetMemberList()
	pq := addon.MqttListSessionsPath + "?page=1&size=" + maxPageSizeStr +
		"&online=" + url.QueryEscape(onlineQ)
	urls := make([]string, len(members))
	for i, m := range members {
		urls[i] = s.urlFor(m, pq)
	}
	results := fetchM(http.MethodGet, urls, nil)

	byID := make(map[string]sessionWire, 256)
	for i, rs := range results {
		if rs.Err != nil || rs.Status != http.StatusOK {
			continue
		}
		var p page[sessionWire]
		if err := json.Unmarshal(rs.Body, &p); err != nil {
			continue
		}
		nodeName := members[i].Name
		for _, item := range p.Items {
			item.Node = nodeName
			existing, has := byID[item.ClientID]
			if !has || (item.Online && !existing.Online) {
				byID[item.ClientID] = item
			}
		}
	}

	merged := make([]sessionWire, 0, len(byID))
	for _, v := range byID {
		merged = append(merged, v)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ClientID < merged[j].ClientID })
	paginate(w, r, merged)
}

// clearSession handles DELETE /api/v1/cluster/mqtt/sessions/{id}.
// Broadcasts to every node; the owning node disconnects the client (online
// case) or evicts the stored session (offline case), and the others 404.
func (s *Rest) clearSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing client id", http.StatusBadRequest)
		return
	}
	path := strings.Replace(addon.MqttClearSessionPath, "{id}", url.PathEscape(id), 1)
	s.broadcastDelete(w, path)
}
