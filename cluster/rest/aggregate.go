package rest

import (
	"encoding/json"
	"net/http"

	addon "github.com/debsahu/comqtt-dashboard/rest"
)

// page is the JSON wire shape returned by each peer's addon REST list
// endpoint. Mirrors addon.Page[T] so we can json.Unmarshal into it without
// dragging in generic type aliasing tricks. T is the concrete row type per
// endpoint.
type page[T any] struct {
	Page  int `json:"page"`
	Size  int `json:"size"`
	Total int `json:"total"`
	Items []T `json:"items"`
}

// maxPageSizeStr mirrors addon.MaxPageSize as a string constant for URL
// building. Each handler asks each peer for size=maxPageSizeStr to pull
// the full local set; the mirror then re-paginates the merged result.
// Keep this in sync if addon.MaxPageSize changes.
const maxPageSizeStr = "500"

// paginate writes a Page[T] envelope to w by applying the request's
// pagination params to the merged items slice. Generic so each endpoint
// gets back its concrete row type.
func paginate[T any](w http.ResponseWriter, r *http.Request, items []T) {
	p := addon.ParsePage(r.URL.Query())
	resp := addon.Page[T]{
		Page:  p.Page,
		Size:  p.Size,
		Total: len(items),
		Items: addon.ApplyPagination(items, p),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// broadcastDelete fans a DELETE out to every peer and writes one consolidated
// response. Returns 204 if any peer succeeded, 404 if every peer returned
// 404, and 502 if every call errored or returned 5xx. Mirrors the addon's
// "find owning node, delete there, no-op elsewhere" semantics without
// needing a routing oracle: each node ignores requests for state it does
// not own (returning 404), so a broadcast is functionally equivalent to a
// targeted dispatch for our payload shapes.
func (s *Rest) broadcastDelete(w http.ResponseWriter, path string) {
	urls := s.peerURLs(path)
	results := fetchM(http.MethodDelete, urls, nil)
	any204, any404, anyOther := false, false, false
	for _, rs := range results {
		switch {
		case rs.Err != nil:
			anyOther = true
		case rs.Status == http.StatusNoContent:
			any204 = true
		case rs.Status == http.StatusNotFound:
			any404 = true
		default:
			anyOther = true
		}
	}
	switch {
	case any204:
		w.WriteHeader(http.StatusNoContent)
	case any404 && !anyOther:
		http.NotFound(w, nil)
	default:
		http.Error(w, "all peers failed", http.StatusBadGateway)
	}
}
