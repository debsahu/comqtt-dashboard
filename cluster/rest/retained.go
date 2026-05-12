package rest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

type retainedWire struct {
	Topic    string `json:"topic"`
	QoS      byte   `json:"qos"`
	Size     int    `json:"size"`
	StoredAt int64  `json:"stored_at"`
	Payload  []byte `json:"payload,omitempty"`
}

// listRetained handles GET /api/v1/cluster/mqtt/retained.
//
// Cluster-wide retained messages are typically broker-wide via the shared
// storage hook (redis-backed in cluster mode), so every node sees the same
// set. We still fan out and dedupe by Topic so memory-only clusters or
// transient inconsistencies don't surprise us.
func (s *Rest) listRetained(w http.ResponseWriter, r *http.Request) {
	topicQ := r.URL.Query().Get("topic")
	payloadQ := r.URL.Query().Get("payload")
	members := s.agent.GetMemberList()
	pq := addon.MqttListRetainedPath + "?page=1&size=" + maxPageSizeStr +
		"&topic=" + url.QueryEscape(topicQ) +
		"&payload=" + url.QueryEscape(payloadQ)
	urls := make([]string, len(members))
	for i, m := range members {
		urls[i] = s.urlFor(m, pq)
	}
	results := fetchM(http.MethodGet, urls, nil)

	seen := make(map[string]struct{}, 64)
	merged := make([]retainedWire, 0, 64)
	for _, rs := range results {
		if rs.Err != nil || rs.Status != http.StatusOK {
			continue
		}
		var p page[retainedWire]
		if err := json.Unmarshal(rs.Body, &p); err != nil {
			continue
		}
		for _, item := range p.Items {
			if _, dup := seen[item.Topic]; dup {
				continue
			}
			seen[item.Topic] = struct{}{}
			merged = append(merged, item)
		}
	}

	if topicQ != "" {
		qLower := strings.ToLower(topicQ)
		filtered := merged[:0]
		for _, item := range merged {
			if strings.Contains(strings.ToLower(item.Topic), qLower) {
				filtered = append(filtered, item)
			}
		}
		merged = filtered
	}

	sort.Slice(merged, func(i, j int) bool { return merged[i].Topic < merged[j].Topic })
	paginate(w, r, merged)
}

// clearRetained handles DELETE /api/v1/cluster/mqtt/retained/{topic}.
//
// Broadcasts to every node so each clears its in-memory retained cache. The
// underlying redis-backed retained store is shared, so the first node to
// process the delete is the one that actually mutates state; the others
// then return 404. The mirror reports 204 if any node returned 204.
func (s *Rest) clearRetained(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	if topic == "" {
		http.Error(w, "missing topic", http.StatusBadRequest)
		return
	}
	path := strings.Replace(addon.MqttClearRetainedPath, "{topic}", url.PathEscape(topic), 1)
	s.broadcastDelete(w, path)
}
