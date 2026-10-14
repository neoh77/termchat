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
	maxMembers   = 256
	memberTTL    = 20 * time.Second
	refreshEvery = 5 * time.Second
	retryWindow  = 60 * time.Second
	retryEvery   = 3 * time.Second
)

type Event struct {
	Kind    string // message, action, peer, or member
	ID      string
	Address string
	Status  string
	Name    string
	Text    string
	Local   bool // true for messages sent by this node
}

type packet struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Text    string `json:"text,omitempty"`
	Port    int    `json:"port,omitempty"`
	Address string `json:"address,omitempty"`
	Token   string `json:"token,omitempty"`
}

type peer struct {
	conn net.Conn
	addr string
	id   string
	name string
	out  chan packet
}

type memberInfo struct {
	name    string
	address string
	seen    time.Time
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
	members  map[string]memberInfo
	seen     map[string]struct{}
	seenList []string
	outgoing map[string]struct{}
	retryFor time.Duration
	retryIn  time.Duration
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
	id, err := newID()
	if err != nil {
		listener.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		Name: name, Port: listener.Addr().(*net.TCPAddr).Port,
		Events: make(chan Event, 1024), id: id,
		listener: listener, ctx: ctx, cancel: cancel,
		peers: make(map[*peer]struct{}), members: make(map[string]memberInfo), seen: make(map[string]struct{}),
		outgoing: make(map[string]struct{}), retryFor: retryWindow, retryIn: retryEvery,
	}
	go n.accept()
	go n.refreshMembers()
	return n, nil
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
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
	n.mu.Lock()
	if n.ctx.Err() != nil {
		n.mu.Unlock()
		return errors.New("node is closed")
	}
	if _, exists := n.outgoing[addr]; exists {
		n.mu.Unlock()
		return nil
	}
	n.outgoing[addr] = struct{}{}
	n.mu.Unlock()
	n.emit(Event{Kind: "peer", Address: addr, Status: "connecting"})
	go n.connectWithRetry(addr)
	return nil
}

func (n *Node) connectWithRetry(addr string) {
	defer func() {
		n.mu.Lock()
		delete(n.outgoing, addr)
		n.mu.Unlock()
	}()
	deadline := time.Now().Add(n.retryFor)
	for {
		if n.ctx.Err() != nil {
			return
		}
		attemptDeadline := time.Now().Add(5 * time.Second)
		if deadline.Before(attemptDeadline) {
			attemptDeadline = deadline
		}
		attempt, cancel := context.WithDeadline(n.ctx, attemptDeadline)
		conn, err := (&net.Dialer{}).DialContext(attempt, "tcp", addr)
		cancel()
		if err == nil {
			connected, _ := n.handle(conn, addr, attemptDeadline)
			if connected {
				// A later disconnect gets a fresh window, even after a long session.
				deadline = time.Now().Add(n.retryFor)
			}
		}
		if n.ctx.Err() != nil {
			return
		}
		if time.Until(deadline) <= 0 {
			n.emit(Event{Kind: "peer", Address: addr, Status: "failed: reconnect timed out"})
			return
		}
		n.emit(Event{Kind: "peer", Address: addr, Status: "reconnecting"})
		timer := time.NewTimer(min(n.retryIn, time.Until(deadline)))
		select {
		case <-timer.C:
		case <-n.ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (n *Node) accept() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			return
		}
		go n.handle(conn, "", time.Now().Add(5*time.Second))
	}
}

func (n *Node) handle(conn net.Conn, address string, handshakeDeadline time.Time) (bool, error) {
	defer conn.Close()
	stopClose := context.AfterFunc(n.ctx, func() { conn.Close() })
	defer stopClose()
	conn.SetDeadline(handshakeDeadline)
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(packet{Type: "hello", ID: n.id, Name: n.Name, Port: n.Port}); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	var hello packet
	if err := json.Unmarshal(scanner.Bytes(), &hello); err != nil || hello.Type != "hello" || !validID(hello.ID) || hello.ID == n.id || hello.Port < 1 || hello.Port > 65535 || len(hello.Name) > 32 || Clean(hello.Name) == "" {
		return false, errors.New("invalid peer handshake (or own address)")
	}
	if address == "" {
		host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		address = net.JoinHostPort(host, strconv.Itoa(hello.Port))
	}
	conn.SetDeadline(time.Time{})
	p := &peer{conn: conn, addr: address, id: hello.ID, name: Clean(hello.Name), out: make(chan packet, maxMembers+64)}
	n.mu.Lock()
	n.peers[p] = struct{}{}
	snapshot := make([]packet, 0, len(n.members))
	for id, member := range n.members {
		if id != p.id {
			snapshot = append(snapshot, packet{Type: "member", ID: id, Name: member.name, Address: member.address})
		}
	}
	n.mu.Unlock()
	n.emit(Event{Kind: "peer", Address: address, Status: "connected (" + p.name + ")"})
	n.announce(packet{Type: "member", ID: p.id, Name: p.name, Address: address}, p)
	for _, member := range snapshot {
		token, err := newID()
		if err != nil {
			continue
		}
		member.Token = token
		select {
		case p.out <- member:
		default:
			conn.Close()
		}
	}
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
readLoop:
	for scanner.Scan() {
		var msg packet
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			break
		}
		switch msg.Type {
		case "chat", "action":
			if !validID(msg.ID) || len(msg.Name) > 32 || Clean(msg.Name) == "" || len(msg.Text) > maxText || Clean(msg.Text) == "" {
				break readLoop
			}
			msg.Name, msg.Text = Clean(msg.Name), Clean(msg.Text)
			n.deliver(msg, p)
		case "member":
			if !validID(msg.ID) || !validID(msg.Token) || len(msg.Name) > 32 || Clean(msg.Name) == "" {
				break readLoop
			}
			canonical, err := Address(msg.Address)
			if err != nil || canonical != msg.Address {
				break readLoop
			}
			msg.Name = Clean(msg.Name)
			n.announce(msg, p)
		default:
			break readLoop
		}
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
	return true, nil
}

func (n *Node) Send(text string) error {
	return n.send("chat", text)
}

// SendAction broadcasts an IRC-style action attributed to this node.
func (n *Node) SendAction(text string) error {
	return n.send("action", text)
}

func (n *Node) send(kind, text string) error {
	text = Clean(text)
	if text == "" || len(text) > maxText {
		return fmt.Errorf("message must be 1–%d bytes", maxText)
	}
	id, err := newID()
	if err != nil {
		return err
	}
	n.deliver(packet{Type: kind, ID: id, Name: n.Name, Text: text}, nil)
	return nil
}

func (n *Node) deliver(msg packet, source *peer) {
	n.mu.Lock()
	if !n.remember("chat:" + msg.ID) {
		n.mu.Unlock()
		return
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
	kind := "message"
	if msg.Type == "action" {
		kind = "action"
	}
	n.emit(Event{Kind: kind, Name: msg.Name, Text: msg.Text, Local: source == nil})
}

// remember must be called with n.mu held.
func (n *Node) remember(key string) bool {
	if _, ok := n.seen[key]; ok {
		return false
	}
	n.seen[key] = struct{}{}
	n.seenList = append(n.seenList, key)
	if len(n.seenList) > maxSeen {
		delete(n.seen, n.seenList[0])
		n.seenList = n.seenList[1:]
	}
	return true
}

// announce forwards a newly observed member update once across the network.
func (n *Node) announce(msg packet, source *peer) {
	if msg.ID == n.id {
		return
	}
	if msg.Token == "" {
		var err error
		msg.Token, err = newID()
		if err != nil {
			return
		}
	}
	n.mu.Lock()
	if !n.remember("member:" + msg.Token) {
		n.mu.Unlock()
		return
	}
	previous, exists := n.members[msg.ID]
	if !exists && len(n.members) >= maxMembers {
		n.mu.Unlock()
		return
	}
	// A directly observed address takes precedence over a relayed address.
	for p := range n.peers {
		if exists && p.id == msg.ID && p != source {
			msg.Name, msg.Address = previous.name, previous.address
			break
		}
	}
	changed := !exists || previous.name != msg.Name || previous.address != msg.Address
	n.members[msg.ID] = memberInfo{name: msg.Name, address: msg.Address, seen: time.Now()}
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
	if changed {
		n.emit(Event{Kind: "member", ID: msg.ID, Name: msg.Name, Address: msg.Address, Status: "present"})
	}
}

func (n *Node) refreshMembers() {
	ticker := time.NewTicker(refreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			n.mu.Lock()
			neighbors := make([]*peer, 0, len(n.peers))
			for p := range n.peers {
				neighbors = append(neighbors, p)
			}
			n.mu.Unlock()
			for _, p := range neighbors {
				n.announce(packet{Type: "member", ID: p.id, Name: p.name, Address: p.addr}, p)
			}
			n.expireMembers(time.Now())
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Node) expireMembers(now time.Time) {
	n.mu.Lock()
	var departed []Event
	for id, member := range n.members {
		if now.Sub(member.seen) <= memberTTL {
			continue
		}
		direct := false
		for p := range n.peers {
			if p.id == id {
				direct = true
				break
			}
		}
		if !direct {
			delete(n.members, id)
			departed = append(departed, Event{Kind: "member", ID: id, Status: "left"})
		}
	}
	n.mu.Unlock()
	for _, event := range departed {
		n.emit(event)
	}
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
