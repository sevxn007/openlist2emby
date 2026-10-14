package emby

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestListenerRejectsOccupiedPortAndStopsWhenDisabled(t *testing.T) {
	occupied, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := occupied.Addr().(*net.TCPAddr).Port

	listener := NewListener(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "emby")
	}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = listener.Shutdown(ctx)
	})

	if err := listener.Reconfigure(t.Context(), true, port); err == nil {
		t.Fatal("Reconfigure() accepted an occupied port")
	}
	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}
	if err := listener.Reconfigure(t.Context(), true, port); err != nil {
		t.Fatalf("Reconfigure() error = %v", err)
	}

	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("listener status = %d", response.StatusCode)
	}

	if err := listener.Reconfigure(t.Context(), false, port); err != nil {
		t.Fatalf("disable listener: %v", err)
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	if response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port)); err == nil {
		_ = response.Body.Close()
		t.Fatal("disabled listener still accepted requests")
	}
}
