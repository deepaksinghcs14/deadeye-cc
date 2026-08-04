package main

import "testing"

func TestWantedChecksum(t *testing.T) {
	body := "abc123  deadeye_darwin_arm64\ndef456  deadeye_linux_amd64\n"
	if got := wantedChecksum(body, "deadeye_linux_amd64"); got != "def456" {
		t.Errorf("got %q", got)
	}
	if got := wantedChecksum(body, "deadeye_windows_amd64.exe"); got != "" {
		t.Errorf("missing asset should yield empty, got %q", got)
	}
}
