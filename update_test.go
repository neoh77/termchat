package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"termchat/internal/chat"
)

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"v0.1.9", "v0.2.0", -1},
		{"v1.0.0", "v0.9.9", 1},
		{"v1.2.3", "v1.2.3", 0},
	} {
		got, err := compareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
	for _, invalid := range []string{"build-123", "v1.2", "v1.02.3", "v1.2.3-beta"} {
		if _, err := compareVersions(invalid, "v1.2.3"); err == nil {
			t.Errorf("accepted invalid version %q", invalid)
		}
	}
}

func TestUpdateLatest(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("release binary is Linux amd64")
	}
	binary := "\x7fELF\x02\x01" + strings.Repeat("\x00", 12) + "\x3e\x00new binary"
	for _, tc := range []struct {
		name, current, tag string
		downloadStatus     int
		wantBinary         string
		wantNotice         string
		wantError          bool
		wantDownloads      int
		invalidBinary      bool
	}{
		{"new release", "v0.1.0", "v0.2.0", http.StatusOK, binary, "Updated to v0.2.0", false, 1, false},
		{"already current", "v0.2.0", "v0.2.0", http.StatusOK, "old binary", "Already up to date", false, 0, false},
		{"newer local version", "v0.3.0", "v0.2.0", http.StatusOK, "old binary", "Already up to date", false, 0, false},
		{"development build", "dev", "v0.2.0", http.StatusOK, binary, "Updated to v0.2.0", false, 1, false},
		{"failed download", "v0.1.0", "v0.2.0", http.StatusInternalServerError, "old binary", "", true, 1, false},
		{"invalid binary", "v0.1.0", "v0.2.0", http.StatusOK, "old binary", "", true, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "termchat")
			if err := os.WriteFile(path, []byte("old binary"), 0755); err != nil {
				t.Fatal(err)
			}
			downloads := 0
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":"termchat","browser_download_url":%q}]}`, tc.tag, server.URL+"/binary")
				case "/binary":
					downloads++
					w.WriteHeader(tc.downloadStatus)
					if tc.downloadStatus == http.StatusOK {
						if tc.invalidBinary {
							fmt.Fprint(w, "not a binary")
						} else {
							fmt.Fprint(w, binary)
						}
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			notice, err := updateLatest(context.Background(), server.Client(), server.URL+"/latest", path, tc.current)
			if (err != nil) != tc.wantError || !strings.Contains(notice, tc.wantNotice) {
				t.Fatalf("updateLatest = %q, %v; want notice containing %q, error %v", notice, err, tc.wantNotice, tc.wantError)
			}
			binary, err := os.ReadFile(path)
			if err != nil || string(binary) != tc.wantBinary {
				t.Errorf("installed binary = %q, %v; want %q", binary, err, tc.wantBinary)
			}
			if downloads != tc.wantDownloads {
				t.Errorf("downloads = %d; want %d", downloads, tc.wantDownloads)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0755 {
				t.Errorf("installed permissions = %v; want 0755", info.Mode().Perm())
			}
		})
	}
}

func TestUpdateCommand(t *testing.T) {
	m := newModel(&chat.Node{Name: "alice", Port: 8085})
	m.input.SetValue("/update")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil || !m.updating || m.input.Value() != "" || m.notice != "Checking for updates…" {
		t.Fatalf("update command did not start: %+v, cmd %v", m, cmd)
	}
	updated, _ = m.Update(updateResult{notice: "Already up to date (v0.1.0)"})
	m = updated.(model)
	if m.updating || m.notice != "Already up to date (v0.1.0)" {
		t.Fatalf("update result was not displayed: %+v", m)
	}
}
