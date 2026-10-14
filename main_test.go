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
		for _, unread := range []int{0, 1, 123456789} {
			m := newModel(&chat.Node{Name: "alice", Port: 8080})
			m.width, m.height = size[0], size[1]
			m.input.Width = m.width - len(m.input.Prompt) - 2
			m.messages = []string{strings.Repeat("hello everyone ", 30), "latest message"}
			m.localIPs = []string{"10.0.0.7", "192.168.1.25"}
			m.order = []string{"192.168.1.100:8080"}
			m.peers[m.order[0]] = "connected (bob)"
			m.newMessages = unread
			view := m.View()
			if width, height := lipgloss.Size(view); width > m.width || height > m.height {
				t.Errorf("terminal %dx%d with %d new messages: view is %dx%d", m.width, m.height, unread, width, height)
			}
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

func TestMeCommandAndActionDisplay(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	n, err := chat.New("alice", port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Close)
	m := newModel(n)
	m.width, m.height = 100, 24
	for _, tc := range []struct {
		input, kind, text string
	}{
		{"/me waves", "action", "waves"},
		{"/me   dances", "action", "dances"},
		{"/meow", "message", "/meow"},
	} {
		m.input.SetValue(tc.input)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		if m.input.Value() != "" {
			t.Fatalf("input was not cleared after %q", tc.input)
		}
		event := <-n.Events
		if event.Kind != tc.kind || event.Text != tc.text {
			t.Fatalf("%q produced %+v; want %s %q", tc.input, event, tc.kind, tc.text)
		}
		updated, _ = m.Update(networkEvent(event))
		m = updated.(model)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "* alice waves") || !strings.Contains(view, "alice: /meow") {
		t.Fatalf("actions and ordinary chat are not displayed correctly: %s", view)
	}
	m.input.SetValue("/me")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.notice == "" || m.input.Value() != "/me" {
		t.Fatalf("empty action should show an error and retain input: %+v", m)
	}
	select {
	case event := <-n.Events:
		t.Fatalf("empty action sent event %+v", event)
	default:
	}
}

func TestNewMessageCount(t *testing.T) {
	m := newModel(&chat.Node{Name: "alice", Port: 8085})
	m.width, m.height = 80, 24
	for _, event := range []networkEvent{
		{Kind: "message", Name: "bob", Text: "hi"},
		{Kind: "action", Name: "bob", Text: "waves"},
		{Kind: "message", Name: "alice", Text: "mine", Local: true},
	} {
		updated, _ := m.Update(event)
		m = updated.(model)
	}
	if m.newMessages != 2 || !strings.Contains(ansi.Strip(m.View()), "Chat (2 new)") {
		t.Fatalf("expected two new messages: count %d, view %s", m.newMessages, m.View())
	}
	if footer := strings.Split(strings.TrimSuffix(ansi.Strip(m.View()), "\n"), "\n"); !strings.Contains(footer[len(footer)-1], "2 NEW MESSAGES · any key clears") {
		t.Fatalf("new-message banner is missing: %s", m.View())
	}
	for _, event := range []tea.Msg{
		tea.WindowSizeMsg{Width: 80, Height: 24},
		networkEvent{Kind: "peer", Address: "127.0.0.1:8085", Status: "connected (bob)"},
	} {
		updated, _ := m.Update(event)
		m = updated.(model)
	}
	if m.newMessages != 2 {
		t.Fatalf("non-key events cleared the count: %d", m.newMessages)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(model)
	if m.newMessages != 0 || strings.Contains(ansi.Strip(m.View()), "Chat (2 new)") {
		t.Fatalf("key interaction did not clear new messages: count %d, view %s", m.newMessages, m.View())
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "NEW MESSAGES") || !strings.Contains(view, "Ctrl+A add peer") {
		t.Fatalf("shortcut hints did not return after clearing the banner: %s", view)
	}
	updated, _ = m.Update(networkEvent{Kind: "message", Name: "bob", Text: "again"})
	m = updated.(model)
	if m.newMessages != 1 || !strings.Contains(ansi.Strip(m.View()), "Chat (1 new)") {
		t.Fatalf("new messages did not resume counting: count %d, view %s", m.newMessages, m.View())
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "1 NEW MESSAGE · any key clears") {
		t.Fatalf("single-message banner is missing: %s", view)
	}
}
