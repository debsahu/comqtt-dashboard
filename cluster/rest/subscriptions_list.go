package rest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

type subscriptionWire struct {
	ClientID          string `json:"client_id"`
	Topic             string `json:"topic"`
	QoS               byte   `json:"qos"`
	NoLocal           bool   `json:"no_local"`
	RetainAsPublished bool   `json:"retain_as_published"`
	Identifier        int    `json:"identifier,omitempty"`
	Node              string `json:"node,omitempty"`
}

// listSubscriptions handles GET /api/v1/cluster/mqtt/subscriptions.
//
// Fan-out + merge across all cluster members. Dedupe key is (ClientID, Topic)
// since the same client may have an entry on its owning node only - cluster
// transitions can temporarily expose duplicates. Query params ?topic= and
// ?clientid= are forwarded.
func (s *Rest) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	topicQ := r.URL.Query().Get("topic")
	clientQ := r.URL.Query().Get("clientid")
	members := s.agent.GetMemberList()
	pq := addon.MqttListSubscriptionsPath + "?page=1&size=" + maxPageSizeStr +
		"&topic=" + url.QueryEscape(topicQ) +
		"&clientid=" + url.QueryEscape(clientQ)
	urls := make([]string, len(members))
	for i, m := range members {
		urls[i] = s.urlFor(m, pq)
	}
	results := fetchM(http.MethodGet, urls, nil)

	type key struct{ cid, topic string }
	seen := make(map[key]struct{}, 256)
	merged := make([]subscriptionWire, 0, 256)
	for i, rs := range results {
		if rs.Err != nil || rs.Status != http.StatusOK {
			continue
		}
		var p page[subscriptionWire]
		if err := json.Unmarshal(rs.Body, &p); err != nil {
			continue
		}
		nodeName := members[i].Name
		for _, item := range p.Items {
			k := key{item.ClientID, item.Topic}
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			item.Node = nodeName
			merged = append(merged, item)
		}
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].ClientID != merged[j].ClientID {
			return merged[i].ClientID < merged[j].ClientID
		}
		return merged[i].Topic < merged[j].Topic
	})
	paginate(w, r, merged)
}
