package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"termchat/internal/chat"
)

// The release build sets this to its SemVer tag with -ldflags.
var version = "dev"

type peerFlags []string

func (p *peerFlags) String() string { return strings.Join(*p, ", ") }
func (p *peerFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

type networkEvent chat.Event

type updateResult struct {
	notice string
}

type memberView struct {
	name    string
	address string
}

type model struct {
	node        *chat.Node
	input       textinput.Model
	adding      bool
	updating    bool
	width       int
	height      int
	messages    []string
	peers       map[string]string
	order       []string
	members     map[string]memberView
	memberOrder []string
	peerScroll  int
	notice      string
	localIPs    []string
	version     string
}

func matchingLocalIPv4(ip net.IP) string {
	ipv4 := ip.To4()
	if ipv4 == nil || (ipv4[0] != 10 && ipv4[0] != 192) {
		return ""
	}
	return ipv4.String()
}

func localIPv4s() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []string
	seen := make(map[string]bool)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil {
				continue
			}
			value := matchingLocalIPv4(ip)
			if value == "" {
				continue
			}
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	sort.Strings(result)
	return result
}

func newModel(n *chat.Node) model {
	input := textinput.New()
	input.Prompt = "Message > "
	input.CharLimit = 4096
	input.Focus()
	return model{node: n, input: input, peers: make(map[string]string), members: make(map[string]memberView), localIPs: localIPv4s(), version: version}
}

func (m model) waitEvent() tea.Msg {
	return networkEvent(<-m.node.Events)
}

func (m model) Init() tea.Cmd { return m.waitEvent }

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case updateResult:
		m.updating = false
		m.notice = msg.notice
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, msg.Width-len(m.input.Prompt)-2)
	case networkEvent:
		event := chat.Event(msg)
		switch event.Kind {
		case "message", "action":
			line := event.Name + ": " + event.Text
			if event.Kind == "action" {
				line = "* " + event.Name + " " + event.Text
			}
			m.messages = append(m.messages, line)
			if len(m.messages) > 1000 {
				m.messages = m.messages[len(m.messages)-1000:]
			}
		case "peer":
			if _, ok := m.peers[event.Address]; !ok {
				m.order = append(m.order, event.Address)
			}
			m.peers[event.Address] = event.Status
		case "member":
			if event.Status == "left" {
				delete(m.members, event.ID)
				for i, id := range m.memberOrder {
					if id == event.ID {
						m.memberOrder = append(m.memberOrder[:i], m.memberOrder[i+1:]...)
						break
					}
				}
			} else {
				if _, ok := m.members[event.ID]; !ok {
					m.memberOrder = append(m.memberOrder, event.ID)
					m.peerScroll = 0
				}
				m.members[event.ID] = memberView{name: event.Name, address: event.Address}
			}
		}
		return m, m.waitEvent
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+q", "ctrl+c":
			return m, tea.Quit
		case "pgup":
			if !m.adding {
				m.peerScroll += max(1, m.height-8)
			}
			return m, nil
		case "pgdown":
			if !m.adding {
				m.peerScroll = max(0, m.peerScroll-max(1, m.height-8))
			}
			return m, nil
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
				if strings.TrimSpace(value) == "/update" {
					if m.updating {
						m.notice = "Update already in progress"
						return m, nil
					}
					m.updating = true
					m.notice = "Checking for updates…"
					m.input.SetValue("")
					return m, func() tea.Msg {
						result, err := updateInstalled()
						if err != nil {
							return updateResult{notice: "Update failed: " + chat.Clean(err.Error())}
						}
						return updateResult{notice: result}
					}
				}
				var err error
				if value == "/me" || strings.HasPrefix(value, "/me ") || strings.HasPrefix(value, "/me\t") {
					err = m.node.SendAction(strings.TrimSpace(value[len("/me"):]))
				} else {
					err = m.node.Send(value)
				}
				if err != nil {
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
	return paneWithScroll(label, lines, width, height, 0)
}

// scroll is the number of display rows above the newest entry.
func paneWithScroll(label string, lines []string, width, height, scroll int) string {
	insideWidth := max(1, width-2)
	insideHeight := max(1, height-2)
	content := []string{title.Render(ansi.Truncate(label, insideWidth, "…"))}
	for _, line := range lines {
		content = append(content, strings.Split(ansi.Wrap(line, insideWidth, " "), "\n")...)
	}
	if len(content) > insideHeight {
		remaining := content[1:]
		visible := insideHeight - 1
		start := max(0, len(remaining)-visible-max(0, scroll))
		content = append(content[:1], remaining[start:start+visible]...)
	}
	return border.Width(insideWidth).Height(insideHeight).Render(strings.Join(content, "\n"))
}

func (m model) View() string {
	if m.width < 40 || m.height < 13 {
		return "Enlarge your terminal to at least 40 columns × 13 rows.\n"
	}
	header := title.Render("termchat") + dim.Render(fmt.Sprintf("  %s · listening on :%d", m.node.Name, m.node.Port))
	header = ansi.Truncate(header, m.width, "…")
	local := "Your IP: none matching 10.* or 192.*"
	if len(m.localIPs) > 0 {
		addresses := make([]string, 0, len(m.localIPs))
		for _, ip := range m.localIPs {
			addresses = append(addresses, fmt.Sprintf("%s:%d", ip, m.node.Port))
		}
		local = "Your IP: " + strings.Join(addresses, ", ")
	}
	peerLines := make([]string, 0, len(m.memberOrder)*2+len(m.order)*2+1)
	for _, address := range m.order {
		status := m.peers[address]
		if strings.HasPrefix(status, "connected") || status == "disconnected" {
			continue
		}
		peerLines = append(peerLines, address, "  "+status)
	}
	for _, id := range m.memberOrder {
		member := m.members[id]
		status := "via group"
		if strings.HasPrefix(m.peers[member.address], "connected") {
			status = "direct"
		}
		peerLines = append(peerLines, member.name+" · "+status, "  "+member.address)
	}
	if len(peerLines) == 0 {
		peerLines = append(peerLines, "Ctrl+A to add a member")
	}
	paneHeight := m.height - 5
	var panes string
	if m.width >= 72 {
		panes = lipgloss.JoinHorizontal(lipgloss.Top,
			pane("Chat", m.messages, m.width-30, paneHeight),
			paneWithScroll(fmt.Sprintf("Members (%d)", len(m.members)), peerLines, 30, paneHeight, m.peerScroll))
	} else {
		panes = paneWithScroll(fmt.Sprintf("Members (%d)", len(m.members)), peerLines, m.width, 5, m.peerScroll) + "\n" + pane("Chat", m.messages, m.width, paneHeight-5)
	}
	status := m.notice
	if status == "" {
		status = fmt.Sprintf("Single room · %d members", len(m.members))
	}
	return strings.Join([]string{
		header,
		title.Render(ansi.Truncate(local, m.width, "…")),
		panes,
		m.input.View(),
		dim.Render(ansi.Truncate(m.version+" · "+status, m.width, "…")),
		dim.Render(ansi.Truncate("Ctrl+A add peer · PgUp/PgDn members · Enter send/connect · Esc cancel · Ctrl+Q quit", m.width, "…")),
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
