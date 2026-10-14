package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"termchat/internal/chat"
)

type peerFlags []string

func (p *peerFlags) String() string { return strings.Join(*p, ", ") }
func (p *peerFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

type networkEvent chat.Event

type model struct {
	node     *chat.Node
	input    textinput.Model
	adding   bool
	width    int
	height   int
	messages []string
	peers    map[string]string
	order    []string
	notice   string
}

func newModel(n *chat.Node) model {
	input := textinput.New()
	input.Prompt = "Message > "
	input.CharLimit = 4096
	input.Focus()
	return model{node: n, input: input, peers: make(map[string]string)}
}

func (m model) waitEvent() tea.Msg {
	return networkEvent(<-m.node.Events)
}

func (m model) Init() tea.Cmd { return m.waitEvent }

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, msg.Width-len(m.input.Prompt)-2)
	case networkEvent:
		event := chat.Event(msg)
		switch event.Kind {
		case "message":
			m.messages = append(m.messages, event.Name+": "+event.Text)
			if len(m.messages) > 1000 {
				m.messages = m.messages[len(m.messages)-1000:]
			}
		case "peer":
			if _, ok := m.peers[event.Address]; !ok {
				m.order = append(m.order, event.Address)
			}
			m.peers[event.Address] = event.Status
		}
		return m, m.waitEvent
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+q", "ctrl+c":
			return m, tea.Quit
		case "ctrl+a":
			m.adding = true
			m.input.SetValue("")
			m.input.Prompt = "Peer IP > "
			m.input.Width = max(10, m.width-len(m.input.Prompt)-2)
			m.notice = fmt.Sprintf("Enter an IP or IP:port; omitted port means %d", chat.DefaultPort)
			return m, nil
		case "esc":
			if m.adding {
				m.adding = false
				m.input.SetValue("")
				m.input.Prompt = "Message > "
				m.input.Width = max(10, m.width-len(m.input.Prompt)-2)
				m.notice = ""
			}
			return m, nil
		case "enter":
			value := m.input.Value()
			if m.adding {
				if err := m.node.AddPeer(value); err != nil {
					m.notice = err.Error()
				} else {
					m.adding = false
					m.input.Prompt = "Message > "
					m.input.Width = max(10, m.width-len(m.input.Prompt)-2)
					m.notice = "Connecting to " + value
					m.input.SetValue("")
				}
			} else if strings.TrimSpace(value) != "" {
				if err := m.node.Send(value); err != nil {
					m.notice = err.Error()
				} else {
					m.input.SetValue("")
					m.notice = ""
				}
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(message)
	return m, cmd
}

var (
	border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63"))
	title  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func pane(label string, lines []string, width, height int) string {
	insideWidth := max(1, width-2)
	insideHeight := max(1, height-2)
	content := []string{title.Render(ansi.Truncate(label, insideWidth, "…"))}
	for _, line := range lines {
		content = append(content, strings.Split(ansi.Wrap(line, insideWidth, " "), "\n")...)
	}
	if len(content) > insideHeight {
		content = append(content[:1], content[len(content)-(insideHeight-1):]...)
	}
	return border.Width(insideWidth).Height(insideHeight).Render(strings.Join(content, "\n"))
}

func (m model) View() string {
	if m.width < 40 || m.height < 13 {
		return "Enlarge your terminal to at least 40 columns × 13 rows.\n"
	}
	header := title.Render("termchat") + dim.Render(fmt.Sprintf("  %s · listening on :%d (share your IP:%d)", m.node.Name, m.node.Port, m.node.Port))
	header = ansi.Truncate(header, m.width, "…")
	peerLines := make([]string, 0, len(m.order)*2+1)
	for _, address := range m.order {
		peerLines = append(peerLines, address, "  "+m.peers[address])
	}
	if len(peerLines) == 0 {
		peerLines = append(peerLines, "Ctrl+A to add a peer")
	}
	paneHeight := m.height - 4
	var panes string
	if m.width >= 72 {
		panes = lipgloss.JoinHorizontal(lipgloss.Top,
			pane("Chat", m.messages, m.width-30, paneHeight),
			pane("Peers", peerLines, 30, paneHeight))
	} else {
		panes = pane("Peers", peerLines, m.width, 5) + "\n" + pane("Chat", m.messages, m.width, paneHeight-5)
	}
	status := m.notice
	if status == "" {
		status = fmt.Sprintf("Single room · %d peer addresses", len(m.order))
	}
	return strings.Join([]string{
		header,
		panes,
		m.input.View(),
		dim.Render(ansi.Truncate(status, m.width, "…")),
		dim.Render(ansi.Truncate("Ctrl+A add peer · Enter send/connect · Esc cancel · Ctrl+Q quit", m.width, "…")),
	}, "\n")
}

func main() {
	name := os.Getenv("USER")
	if name == "" {
		name = "anon"
	}
	nameFlag := flag.String("name", name, "chat nickname")
	port := flag.Int("port", 0, fmt.Sprintf("listening port (default: try %d, then %d)", chat.DefaultPort, chat.FallbackPort))
	var peers peerFlags
	flag.Var(&peers, "peer", "peer IP or IP:port (repeatable)")
	flag.Parse()
	node, err := chat.New(*nameFlag, *port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "termchat:", err)
		os.Exit(1)
	}
	defer node.Close()
	for _, address := range peers {
		if err := node.AddPeer(address); err != nil {
			fmt.Fprintln(os.Stderr, "termchat: invalid --peer:", err)
			os.Exit(1)
		}
	}
	if _, err := tea.NewProgram(newModel(node), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "termchat:", err)
		os.Exit(1)
	}
}
