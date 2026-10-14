package openlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMediaLibraryClientListsWithRefreshAndResolvesFreshDownloadURL(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "synthetic-admin-key" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/fs/list":
			assertJSONBody(t, r, map[string]any{
				"path": "/Movies", "page": float64(1), "per_page": float64(0), "refresh": true,
			})
			writeEnvelope(t, w, http.StatusOK, map[string]any{"content": []map[string]any{{
				"name": "Dune.mkv", "size": 42, "is_dir": false,
				"modified": "2026-07-27T08:00:00Z", "thumb": "/images/dune.jpg",
			}}})
		case "/api/fs/get":
			if got := r.UserAgent(); got != "SyntheticPlayer/1.0" {
				t.Fatalf("User-Agent = %q", got)
			}
			assertJSONBody(t, r, map[string]any{"path": "/Movies/Dune.mkv"})
			writeEnvelope(t, w, http.StatusOK, map[string]any{
				"is_dir": false, "raw_url": "/raw/dune.mkv?sign=fresh",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewMediaLibraryClient(NewClient(server.Client()), server.URL, "synthetic-admin-key")
	entries, err := client.ListDirectory(t.Context(), "/Movies", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "Dune.mkv" || entries[0].ModifiedAt == nil || entries[0].Thumbnail != server.URL+"/images/dune.jpg" {
		t.Fatalf("entries = %#v", entries)
	}
	target, err := client.ResolveDownloadURL(t.Context(), "/Movies/Dune.mkv", "SyntheticPlayer/1.0")
	if err != nil {
		t.Fatal(err)
	}
	if target != server.URL+"/raw/dune.mkv?sign=fresh" {
		t.Fatalf("target = %q", target)
	}
}

func assertJSONBody(t *testing.T, request *http.Request, want map[string]any) {
	t.Helper()
	defer request.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request body = %#v, want %#v", got, want)
	}
}

func writeEnvelope(t *testing.T, writer http.ResponseWriter, code int, data any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(map[string]any{
		"code": code, "message": "success", "data": data,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMediaLibraryClientValidatesPathsBeforeRequesting(t *testing.T) {
	client := NewMediaLibraryClient(NewClient(&http.Client{
		Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid path reached OpenList")
			return nil, nil
		}),
	}), "http://openlist.example", "synthetic-admin-key")
	for _, directory := range []string{"", "Movies"} {
		if _, err := client.ListDirectory(t.Context(), directory, false); err == nil {
			t.Errorf("ListDirectory(%q) accepted an invalid path", directory)
		}
	}
	for _, objectPath := range []string{"", "Movies/Dune.mkv", "/", "/Movies/.."} {
		if _, err := client.ResolveDownloadURL(t.Context(), objectPath, ""); err == nil {
			t.Errorf("ResolveDownloadURL(%q) accepted an invalid path", objectPath)
		}
	}
}

func TestMediaLibraryClientMapsUpstreamFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   int
	}{
		{"empty response", http.StatusOK, "", http.StatusBadGateway},
		{"malformed response", http.StatusOK, "not-json", http.StatusBadGateway},
		{"invalid data", http.StatusOK, `{"code":200,"data":"invalid"}`, http.StatusBadGateway},
		{"unauthorized HTTP", http.StatusUnauthorized, "private upstream body", http.StatusUnauthorized},
		{"forbidden envelope", http.StatusOK, `{"code":403,"message":"denied"}`, http.StatusForbidden},
		{"upstream unavailable", http.StatusServiceUnavailable, "private upstream body", http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := NewMediaLibraryClient(NewClient(server.Client()), server.URL, "synthetic-admin-key")
			_, listErr := client.ListDirectory(t.Context(), "/Movies", true)
			_, downloadErr := client.ResolveDownloadURL(t.Context(), "/Movies/Dune.mkv", "SyntheticPlayer/1.0")
			for _, err := range []error{listErr, downloadErr} {
				var upstreamError *Error
				if !errors.As(err, &upstreamError) || upstreamError.StatusCode() != test.want {
					t.Fatalf("upstream error = %v, want status %d", err, test.want)
				}
				if strings.Contains(upstreamError.PublicMessage(), "private upstream body") || strings.Contains(upstreamError.PublicMessage(), "synthetic-admin-key") {
					t.Fatalf("public error exposed private values: %s", upstreamError.PublicMessage())
				}
			}
		})
	}
}

func TestMediaLibraryClientRejectsInvalidPlaybackURLs(t *testing.T) {
	for _, data := range []map[string]any{
		{"is_dir": true, "raw_url": "/Movies"},
		{"is_dir": false, "raw_url": " "},
		{"is_dir": false, "raw_url": "file:///Movies/Dune.mkv"},
	} {
		t.Run(fmt.Sprint(data), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeEnvelope(t, w, http.StatusOK, data)
			}))
			defer server.Close()
			client := NewMediaLibraryClient(NewClient(server.Client()), server.URL, "synthetic-admin-key")
			_, err := client.ResolveDownloadURL(t.Context(), "/Movies/Dune.mkv", "")
			var upstreamError *Error
			if !errors.As(err, &upstreamError) || upstreamError.StatusCode() != http.StatusBadGateway {
				t.Fatalf("playback URL error = %v", err)
			}
		})
	}
}

func TestMediaLibraryClientPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := NewMediaLibraryClient(nil, "http://openlist.example", "synthetic-admin-key")
	_, listErr := client.ListDirectory(ctx, "/", true)
	_, downloadErr := client.ResolveDownloadURL(ctx, "/Movies/Dune.mkv", "SyntheticPlayer/1.0")
	for _, err := range []error{listErr, downloadErr} {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled request error = %v", err)
		}
	}
}
