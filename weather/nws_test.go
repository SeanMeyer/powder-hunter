package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setNWSBaseURL overrides the NWS base URL for testing and returns the original.
func setNWSBaseURL(url string) {
	nwsBaseURL = url
}

func TestNWSClient_FetchAFD(t *testing.T) {
	t.Run("happy path — fetches latest AFD text", func(t *testing.T) {
		mux := http.NewServeMux()

		// List AFDs endpoint.
		mux.HandleFunc("/products/types/AFD/locations/SLC", func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"@graph": []map[string]any{
					{
						"id":           "abc-123",
						"issuanceTime": "2026-03-04T18:00:00+00:00",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})

		// Fetch product endpoint.
		mux.HandleFunc("/products/abc-123", func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"productText": "...WINTER STORM WARNING REMAINS IN EFFECT...\nHeavy snow expected above 7000 ft.",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		client := &NWSClient{
			client:    server.Client(),
			gridCache: make(map[string]nwsGrid),
		}
		// Override base URL for testing.
		origBaseURL := nwsBaseURL
		defer func() { setNWSBaseURL(origBaseURL) }()
		setNWSBaseURL(server.URL)

		afd, err := client.FetchAFD(context.Background(), "SLC")
		if err != nil {
			t.Fatalf("FetchAFD failed: %v", err)
		}

		if afd.WFO != "SLC" {
			t.Errorf("WFO = %q, want SLC", afd.WFO)
		}
		if afd.Text == "" {
			t.Error("expected non-empty discussion text")
		}
		if afd.IssuedAt.IsZero() {
			t.Error("expected non-zero IssuedAt")
		}
		if afd.FetchedAt.IsZero() {
			t.Error("expected non-zero FetchedAt")
		}
		t.Logf("AFD text: %s", afd.Text[:50])
	})

	t.Run("empty WFO returns error", func(t *testing.T) {
		client := NewNWSClient(http.DefaultClient)
		_, err := client.FetchAFD(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty WFO")
		}
	})

	t.Run("no AFD products returns error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/products/types/AFD/locations/XXX", func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{"@graph": []map[string]any{}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		client := &NWSClient{client: server.Client(), gridCache: make(map[string]nwsGrid)}
		origBaseURL := nwsBaseURL
		defer func() { setNWSBaseURL(origBaseURL) }()
		setNWSBaseURL(server.URL)

		_, err := client.FetchAFD(context.Background(), "XXX")
		if err == nil {
			t.Error("expected error when no AFD products found")
		}
	})

	t.Run("API error returns error gracefully", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/products/types/AFD/locations/ERR", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		client := &NWSClient{client: server.Client(), gridCache: make(map[string]nwsGrid)}
		origBaseURL := nwsBaseURL
		defer func() { setNWSBaseURL(origBaseURL) }()
		setNWSBaseURL(server.URL)

		_, err := client.FetchAFD(context.Background(), "ERR")
		if err == nil {
			t.Error("expected error for 500 response")
		}
	})
}
