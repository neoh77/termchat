package main

import (
	"net"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestViewKeepsVersionVisibleWithNotice(t *testing.T) {
	m := newModel(&chat.Node{Name: "alice", Port: 8085})
	m.width, m.height = 40, 13
	m.notice = "connection failed: timed out"
	view := ansi.Strip(m.View())
	if !strings.Contains(view, version+" · connection") {
		t.Fatalf("version is not visible with a notice: %s", view)
	}
}

func TestMembersPanelShowsLateJoinAndScrolls(t *testing.T) {
	m := newModel(&chat.Node{Name: "alice", Port: 8085})
	m.width, m.height = 40, 13
	for _, member := range []struct{ id, name, address string }{
		{"bob", "bob", "10.0.0.2:8085"},
		{"charlie", "charlie", "10.0.0.3:8085"},
		{"dana", "dana", "10.0.0.4:8085"},
	} {
		updated, _ := m.Update(networkEvent{Kind: "member", ID: member.id, Name: member.name, Address: member.address, Status: "present"})
		m = updated.(model)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "dana · via group") || !strings.Contains(view, "Members (3)") {
		t.Fatalf("new member is missing: %s", view)
	}
	updated, _ := m.Update(networkEvent{Kind: "peer", Address: "10.0.0.4:8085", Status: "connected (dana)"})
	m = updated.(model)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "dana · direct") {
		t.Fatalf("direct member is not identified: %s", view)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(model)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "bob · via group") {
		t.Fatalf("older member is not reachable by scrolling: %s", view)
	}
}
