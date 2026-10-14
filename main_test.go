package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"termchat/internal/chat"
)

func TestViewFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{40, 13}, {65, 20}, {80, 24}, {120, 30}} {
		m := newModel(&chat.Node{Name: "alice", Port: 8080})
		m.width, m.height = size[0], size[1]
		m.input.Width = m.width - len(m.input.Prompt) - 2
		m.messages = []string{strings.Repeat("hello everyone ", 30), "latest message"}
		m.order = []string{"192.168.1.100:8080"}
		m.peers[m.order[0]] = "connected (bob)"
		view := m.View()
		if width, height := lipgloss.Size(view); width > m.width || height > m.height {
			t.Errorf("terminal %dx%d: view is %dx%d", m.width, m.height, width, height)
		}
	}
}
