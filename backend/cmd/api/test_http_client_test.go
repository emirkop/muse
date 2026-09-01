package main

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// Keep-alives are disabled on purpose. This package starts one httptest server
// per test, so hundreds of servers bind and release ephemeral ports over a run;
// a process-wide pool can hold an idle connection to a port a later server
// rebinds, which fails in a way that looks like a product bug.
var testHTTPClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

func testGet(url string) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return testHTTPClient.Do(request)
}

func testPostJSON(url string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	return testHTTPClient.Do(request)
}

// A test that cannot reach its own server should say so here, at the point of
// construction, rather than failing later on whichever request happens to be
// first — which reads like a product defect and is what a shared connection
// pool across recycled ephemeral ports once produced in CI.
func requireServing(t *testing.T, baseURL string) {
	t.Helper()
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		response, err := testGet(baseURL + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("health returned %d", response.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("test server at %s never served /health: %v", baseURL, lastErr)
}
