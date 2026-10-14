package chat

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPort  = 8085
	FallbackPort = 8080
	maxText      = 4096
	maxFrame     = 8192
	maxSeen      = 8192
)

type Event struct {
	Kind    string // message, peer, or error
	Address string
	Status  string
	Name    string
	Text    string
}

type packet struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
	Port int    `json:"port,omitempty"`
}

type peer struct {
	conn net.Conn
	addr string
	out  chan packet
}

// Node is one participant in a single, forwarded chat room.
type Node struct {
	Name   string
	Port   int
	Events chan Event

	id       string
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	peers    map[*peer]struct{}
	seen     map[string]struct{}
	seenList []string
}

func New(name string, port int) (*Node, error) {
	name = Clean(name)
	if name == "" || len(name) > 32 {
		return nil, errors.New("nickname must be 1–32 characters")
	}
	if port < 0 || port > 65535 {
		return nil, errors.New("port must be between 1 and 65535")
	}
	chosen := port
	if chosen == 0 {
		chosen = DefaultPort
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(chosen)))
	if err != nil && port == 0 {
		listener, err = net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(FallbackPort)))
	}
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		listener.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		Name: name, Port: listener.Addr().(*net.TCPAddr).Port,
		Events: make(chan Event, 1024), id: hex.EncodeToString(idBytes),
		listener: listener, ctx: ctx, cancel: cancel,
		peers: make(map[*peer]struct{}), seen: make(map[string]struct{}),
	}
	go n.accept()
	return n, nil
}

// Clean removes control characters so network input cannot issue terminal escape sequences.
func Clean(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s))
}

// Address accepts an IP, IP:port, or [IPv6]:port. An omitted port uses DefaultPort.
func Address(input string) (string, error) {
	input = strings.TrimSpace(input)
	if ip := net.ParseIP(input); ip != nil {
		return net.JoinHostPort(ip.String(), strconv.Itoa(DefaultPort)), nil
	}
	host, portText, err := net.SplitHostPort(input)
	if err != nil {
		// An IPv4 address without a port is handled above.
		return "", errors.New("enter an IP address or IP:port")
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(portText)
	if ip == nil || err != nil || port < 1 || port > 65535 {
		return "", errors.New("enter a valid IP and port (1–65535)")
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
}

// AddPeer connects in the background so the UI remains responsive.
func (n *Node) AddPeer(address string) error {
	addr, err := Address(address)
	if err != nil {
		return err
	}
	n.emit(Event{Kind: "peer", Address: addr, Status: "connecting"})
	go func() {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(n.ctx, "tcp", addr)
		if err != nil {
			n.emit(Event{Kind: "peer", Address: addr, Status: "failed: " + Clean(err.Error())})
			return
		}
		n.handle(conn, addr)
	}()
	return nil
}

func (n *Node) accept() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			return
		}
		go n.handle(conn, "")
	}
}

func (n *Node) handle(conn net.Conn, address string) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(packet{Type: "hello", ID: n.id, Name: n.Name, Port: n.Port}); err != nil {
		n.failed(address, err)
		return
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	if !scanner.Scan() {
		n.failed(address, scanner.Err())
		return
	}
	var hello packet
	if err := json.Unmarshal(scanner.Bytes(), &hello); err != nil || hello.Type != "hello" || hello.ID == "" || hello.ID == n.id || hello.Port < 1 || hello.Port > 65535 || len(hello.Name) > 32 || Clean(hello.Name) == "" {
		n.failed(address, errors.New("invalid peer handshake (or own address)"))
		return
	}
	if address == "" {
		host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		address = net.JoinHostPort(host, strconv.Itoa(hello.Port))
	}
	conn.SetDeadline(time.Time{})
	p := &peer{conn: conn, addr: address, out: make(chan packet, 64)}
	n.mu.Lock()
	n.peers[p] = struct{}{}
	n.mu.Unlock()
	n.emit(Event{Kind: "peer", Address: address, Status: "connected (" + Clean(hello.Name) + ")"})
	writerDone := make(chan struct{})
	stopWriter := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case msg := <-p.out:
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if encoder.Encode(msg) != nil {
					conn.Close()
					return
				}
			case <-n.ctx.Done():
				return
			case <-stopWriter:
				return
			}
		}
	}()
	for scanner.Scan() {
		var msg packet
		if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.Type != "chat" || len(msg.ID) != 32 || len(msg.Name) > 32 || Clean(msg.Name) == "" || len(msg.Text) > maxText || Clean(msg.Text) == "" {
			break
		}
		msg.Name, msg.Text = Clean(msg.Name), Clean(msg.Text)
		n.deliver(msg, p)
	}
	conn.Close()
	close(stopWriter)
	<-writerDone
	n.mu.Lock()
	delete(n.peers, p)
	remaining := false
	for other := range n.peers {
		if other.addr == address {
			remaining = true
			break
		}
	}
	n.mu.Unlock()
	if !remaining {
		n.emit(Event{Kind: "peer", Address: address, Status: "disconnected"})
	}
}

func (n *Node) failed(address string, err error) {
	if address != "" {
		message := "connection closed"
		if err != nil {
			message = Clean(err.Error())
		}
		n.emit(Event{Kind: "peer", Address: address, Status: "failed: " + message})
	}
}

func (n *Node) Send(text string) error {
	text = Clean(text)
	if text == "" || len(text) > maxText {
		return fmt.Errorf("message must be 1–%d bytes", maxText)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}
	n.deliver(packet{Type: "chat", ID: hex.EncodeToString(idBytes), Name: n.Name, Text: text}, nil)
	return nil
}

func (n *Node) deliver(msg packet, source *peer) {
	n.mu.Lock()
	if _, ok := n.seen[msg.ID]; ok {
		n.mu.Unlock()
		return
	}
	n.seen[msg.ID] = struct{}{}
	n.seenList = append(n.seenList, msg.ID)
	if len(n.seenList) > maxSeen {
		delete(n.seen, n.seenList[0])
		n.seenList = n.seenList[1:]
	}
	for p := range n.peers {
		if p != source {
			select {
			case p.out <- msg:
			default:
				p.conn.Close() // A stalled peer must not block the whole room.
			}
		}
	}
	n.mu.Unlock()
	n.emit(Event{Kind: "message", Name: msg.Name, Text: msg.Text})
}

func (n *Node) emit(event Event) {
	select {
	case n.Events <- event:
	case <-n.ctx.Done():
	}
}

func (n *Node) Close() {
	n.cancel()
	n.listener.Close()
	n.mu.Lock()
	for p := range n.peers {
		p.conn.Close()
	}
	n.mu.Unlock()
}
