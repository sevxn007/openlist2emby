package openlist

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type proxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn proxyRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestProxyJSONTimeouts(t *testing.T) {
	for _, test := range []struct {
		name        string
		path        string
		delay       time.Duration
		cancelAfter time.Duration
		wantErr     error
	}{
		{
			name:  "batch rename can exceed the ordinary request timeout",
			path:  "/api/fs/batch_rename",
			delay: 25 * time.Second,
		},
		{
			name:    "ordinary requests retain their timeout",
			path:    "/api/fs/list",
			delay:   25 * time.Second,
			wantErr: context.DeadlineExceeded,
		},
		{
			name:    "batch rename retains a finite timeout",
			path:    "/api/fs/batch_rename",
			delay:   31 * time.Minute,
			wantErr: context.DeadlineExceeded,
		},
		{
			name:        "batch rename respects caller cancellation",
			path:        "/api/fs/batch_rename",
			delay:       25 * time.Second,
			cancelAfter: time.Second,
			wantErr:     context.Canceled,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const responseBody = `{"code":200,"message":"success","data":null}`
				client := NewClient(nil)
				transport := proxyRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					select {
					case <-time.After(test.delay):
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(responseBody)),
							Header:     make(http.Header),
						}, nil
					case <-request.Context().Done():
						return nil, request.Context().Err()
					}
				})
				client.httpClient.Transport = transport
				client.transferClient.Transport = transport
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancelAfter > 0 {
					time.AfterFunc(test.cancelAfter, cancel)
				}

				body, err := client.ProxyJSON(ctx, "http://openlist.test", test.path, "synthetic-admin-key", []byte(`{}`))
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("ProxyJSON error = %v, want %v", err, test.wantErr)
				}
				if test.wantErr != nil {
					var upstreamErr *Error
					if !errors.As(err, &upstreamErr) || upstreamErr.StatusCode() != http.StatusBadGateway || upstreamErr.PublicMessage() != "openlist request failed" {
						t.Fatalf("unexpected public error: %v", err)
					}
					return
				}
				if string(body) != responseBody {
					t.Fatalf("response = %s, want %s", body, responseBody)
				}
			})
		})
	}
}
