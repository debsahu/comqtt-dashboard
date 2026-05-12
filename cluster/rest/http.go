package rest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// fetchTimeout is the per-request deadline for a peer call. Upstream uses 3s
// for the same purpose; we mirror it to keep the cluster's request behavior
// uniform across REST surfaces.
const fetchTimeout = 3 * time.Second

// peerResult captures the outcome of a single peer call.
type peerResult struct {
	URL    string
	Status int
	Body   []byte
	Err    error
}

// fetchM issues parallel HTTP requests to the supplied URLs and returns one
// result per URL. Order is preserved to match urls[i] <-> results[i].
//
// Local re-implementation of upstream `cluster/rest/.fetchM`, which is
// unexported (see TODO.upstream.md for the eventual upstream PR to export
// it). The two implementations are intentionally near-identical so behavior
// stays uniform if upstream's later changes.
func fetchM(method string, urls []string, body []byte) []peerResult {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	results := make([]peerResult, len(urls))
	var wg sync.WaitGroup
	wg.Add(len(urls))
	for i, url := range urls {
		go func(i int, url string) {
			defer wg.Done()
			results[i] = fetch(ctx, method, url, body)
		}(i, url)
	}
	wg.Wait()
	return results
}

func fetch(ctx context.Context, method, url string, data []byte) peerResult {
	rs := peerResult{URL: url}
	var body io.Reader
	if data != nil {
		body = bytes.NewBuffer(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		rs.Err = err
		return rs
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			rs.Err = context.DeadlineExceeded
		} else {
			rs.Err = err
		}
		return rs
	}
	defer resp.Body.Close()
	rs.Status = resp.StatusCode
	rs.Body, rs.Err = io.ReadAll(resp.Body)
	return rs
}
