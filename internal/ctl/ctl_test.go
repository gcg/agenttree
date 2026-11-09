package ctl

import (
	"path/filepath"
	"testing"
	"time"
)

func TestServerClientRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctl.sock")
	srv, err := StartServer(sock)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer srv.Close()

	want := Message{Kind: "hook", Feature: "auth", Event: "blocked"}
	if err := Send(sock, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case got := <-srv.Messages():
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}
