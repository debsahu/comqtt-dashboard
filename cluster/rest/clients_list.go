package rest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

// clientWire mirrors the addon's clientSummary JSON shape with a Node field
// added so the dashboard can show which cluster member each client lives on.
// Field tags match the addon's exact JSON keys.
type clientWire struct {
	ClientID    string `json:"client_id"`
	Username    string `json:"username"`
	Remote      string `json:"remote"`
	ConnectedAt int64  `json:"connected_at"`
	Keepalive   uint16 `json:"keepalive"`
	Subs        int    `json:"subs"`
	Pending     int    `json:"pending"`
	Node        string `json:"node,omitempty"`
}

// listClients handles GET /api/v1/cluster/mqtt/clients.
//
// Fans out to every cluster member's /api/v1/mqtt/clients endpoint, merges
// the per-node items, dedupes by ClientID (last writer wins; should not
// happen but cluster transitions can briefly show a client on two nodes),
// sorts by ClientID, and applies the request's pagination params on the
// merged slice.
//
// Query params: ?q= substring filter on ClientID (passed through to each
// peer; the merged result is also filtered locally to defend against a
// peer ignoring the filter).
func (s *Rest) listClients(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	members := s.agent.GetMemberList()
	pq := addon.MqttListClientsPath + "?page=1&size=" + maxPageSizeStr + "&q=" + url.QueryEscape(q)
	urls := make([]string, len(members))
	for i, m := range members {
		urls[i] = s.urlFor(m, pq)
	}
	results := fetchM(http.MethodGet, urls, nil)

	seen := make(map[string]struct{}, 256)
	merged := make([]clientWire, 0, 256)
	for i, rs := range results {
		if rs.Err != nil || rs.Status != http.StatusOK {
			continue
		}
		var p page[clientWire]
		if err := json.Unmarshal(rs.Body, &p); err != nil {
			continue
		}
		nodeName := members[i].Name
		for _, item := range p.Items {
			if _, dup := seen[item.ClientID]; dup {
				continue
			}
			seen[item.ClientID] = struct{}{}
			item.Node = nodeName
			merged = append(merged, item)
		}
	}

	if q != "" {
		qLower := strings.ToLower(q)
		filtered := merged[:0]
		for _, c := range merged {
			if strings.Contains(strings.ToLower(c.ClientID), qLower) {
				filtered = append(filtered, c)
			}
		}
		merged = filtered
	}

	sort.Slice(merged, func(i, j int) bool { return merged[i].ClientID < merged[j].ClientID })
	paginate(w, r, merged)
}
