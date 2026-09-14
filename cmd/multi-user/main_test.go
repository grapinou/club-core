package main

import (
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	original := map[string]string{"A": "local-placeholder"}
	if err := privateJSON(p, original); err != nil {
		t.Fatal(err)
	}
	if err := privateJSON(p, map[string]string{"A": "replacement"}); err == nil {
		t.Fatal("overwrote private state")
	}
	var got map[string]string
	if err := readPrivate(p, &got); err != nil || got["A"] != original["A"] {
		t.Fatal("private state changed or unreadable")
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if readPrivate(p, &got) == nil {
		t.Fatal("accepted public file")
	}
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if readPrivate(link, &got) == nil {
		t.Fatal("accepted symlink")
	}
}
func TestCampaignPortsStayLocal(t *testing.T) {
	p := network.MustParsePort("5432/tcp")
	h := container.HostConfig{PortBindings: network.PortMap{p: {{HostIP: netip.IPv4Unspecified(), HostPort: "5432"}}}}
	loopback(&h)
	if !h.PortBindings[p][0].HostIP.IsLoopback() || h.PortBindings[p][0].HostPort != "5432" {
		t.Fatal("port exposed outside loopback or changed")
	}
}
