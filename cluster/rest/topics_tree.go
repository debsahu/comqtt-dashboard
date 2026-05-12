package rest

import (
	"encoding/json"
	"net/http"
	"sort"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

// topicNode is the recursive JSON shape returned by the addon's topicsTree
// endpoint. Re-declared here because the addon's version is unexported.
type topicNode struct {
	Topic       string       `json:"topic"`
	Subscribers int          `json:"subscribers"`
	Children    []*topicNode `json:"children,omitempty"`
}

// topicsTree handles GET /api/v1/cluster/mqtt/topics.
//
// Fans out to every member's /api/v1/mqtt/topics, then merges the per-node
// tries into one. Subscriber counts are summed: if node-A reports 3
// subscribers on "foo/bar" and node-B reports 2, the merged tree reports 5.
func (s *Rest) topicsTree(w http.ResponseWriter, r *http.Request) {
	urls := s.peerURLs(addon.MqttTopicsTreePath)
	results := fetchM(http.MethodGet, urls, nil)

	merged := &topicNode{}
	for _, rs := range results {
		if rs.Err != nil || rs.Status != http.StatusOK {
			continue
		}
		var peer topicNode
		if err := json.Unmarshal(rs.Body, &peer); err != nil {
			continue
		}
		mergeTopic(merged, &peer)
	}
	sortTopicTree(merged)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(merged)
}

// mergeTopic merges src into dst recursively. dst's Subscribers gets the
// sum; children are merged by Topic field equality.
func mergeTopic(dst, src *topicNode) {
	dst.Subscribers += src.Subscribers
	for _, srcChild := range src.Children {
		var match *topicNode
		for _, dstChild := range dst.Children {
			if dstChild.Topic == srcChild.Topic {
				match = dstChild
				break
			}
		}
		if match == nil {
			match = &topicNode{Topic: srcChild.Topic}
			dst.Children = append(dst.Children, match)
		}
		mergeTopic(match, srcChild)
	}
}

func sortTopicTree(n *topicNode) {
	sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Topic < n.Children[j].Topic })
	for _, c := range n.Children {
		sortTopicTree(c)
	}
}
