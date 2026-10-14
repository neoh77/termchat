package main

import (
	"net"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"termchat/internal/chat"
)

func TestViewFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{40, 13}, {65, 20}, {80, 24}, {120, 30}} {
		m := newModel(&chat.Node{Name: "alice", Port: 8080})
		m.width, m.height = size[0], size[1]
		m.input.Width = m.width - len(m.input.Prompt) - 2
		m.messages = []string{strings.Repeat("hello everyone ", 30), "latest message"}
		m.localIPs = []string{"10.0.0.7", "192.168.1.25"}
		m.order = []string{"192.168.1.100:8080"}
		m.peers[m.order[0]] = "connected (bob)"
		view := m.View()
		if width, height := lipgloss.Size(view); width > m.width || height > m.height {
			t.Errorf("terminal %dx%d: view is %dx%d", m.width, m.height, width, height)
		}
	}
}

func TestMatchingLocalIPv4(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"10.1.2.3", "10.1.2.3"},
		{"192.0.2.10", "192.0.2.10"},
		{"192.168.1.25", "192.168.1.25"},
		{"172.16.0.1", ""},
		{"127.0.0.1", ""},
		{"2001:db8::1", ""},
	} {
		if got := matchingLocalIPv4(net.ParseIP(tc.input)); got != tc.want {
			t.Errorf("matchingLocalIPv4(%s) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestViewShowsLocalAddressAndListeningPort(t *testing.T) {
	m := newModel(&chat.Node{Name: "alice", Port: 8080})
	m.width, m.height = 100, 24
	m.localIPs = []string{"10.0.0.7", "192.168.1.25"}
	view := ansi.Strip(m.View())
	for _, address := range []string{"10.0.0.7:8080", "192.168.1.25:8080"} {
		if !strings.Contains(view, address) {
			t.Errorf("view is missing local address %s", address)
		}
	}
}
