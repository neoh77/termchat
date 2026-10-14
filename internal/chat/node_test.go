package chat

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestAddress(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"127.0.0.1", "127.0.0.1:8085"},
		{"127.0.0.1:8080", "127.0.0.1:8080"},
		{"::1", "[::1]:8085"},
		{"[::1]:8080", "[::1]:8080"},
	}
	for _, tc := range cases {
		got, err := Address(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("Address(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{"example.com", "127.0.0.1:0", "127.0.0.1:65536", "not-an-ip:80"} {
		if _, err := Address(input); err == nil {
			t.Errorf("Address(%q) should fail", input)
		}
	}
}

func TestCleanRemovesTerminalControl(t *testing.T) {
	if got := Clean(" hello\x1b[31m\nworld "); got != "hello[31mworld" {
		t.Fatalf("Clean returned %q", got)
	}
}

func newTestNode(t *testing.T, name string) *Node {
	t.Helper()
	// Explicitly choose a free port instead of binding to the default port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	n, err := New(name, port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Close)
	return n
}

func await(t *testing.T, n *Node, kind string) Event {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-n.Events:
			if event.Kind == kind && (kind != "peer" || len(event.Status) >= 9 && event.Status[:9] == "connected") {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s on %s", kind, n.Name)
		}
	}
}

func TestMeshForwardsOnceAroundCycle(t *testing.T) {
	a := newTestNode(t, "alice")
	b := newTestNode(t, "bob")
	c := newTestNode(t, "charlie")
	for _, edge := range []struct{ from, to *Node }{{a, b}, {b, c}, {c, a}} {
		if err := edge.from.AddPeer(fmt.Sprintf("127.0.0.1:%d", edge.to.Port)); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []*Node{a, b, c} {
		await(t, n, "peer")
		await(t, n, "peer")
	}
	if err := a.Send("hello everyone"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*Node{a, b, c} {
		msg := await(t, n, "message")
		if msg.Name != "alice" || msg.Text != "hello everyone" {
			t.Errorf("%s received %+v", n.Name, msg)
		}
	}
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case event := <-a.Events:
			if event.Kind == "message" {
				t.Fatal("duplicate message at alice")
			}
		case event := <-b.Events:
			if event.Kind == "message" {
				t.Fatal("duplicate message at bob")
			}
		case event := <-c.Events:
			if event.Kind == "message" {
				t.Fatal("duplicate message at charlie")
			}
		case <-deadline:
			return
		}
	}
}

func TestForwardsAcrossIndirectPeer(t *testing.T) {
	a := newTestNode(t, "alice")
	b := newTestNode(t, "bob")
	c := newTestNode(t, "charlie")
	if err := a.AddPeer(fmt.Sprintf("127.0.0.1:%d", b.Port)); err != nil {
		t.Fatal(err)
	}
	if err := b.AddPeer(fmt.Sprintf("127.0.0.1:%d", c.Port)); err != nil {
		t.Fatal(err)
	}
	await(t, a, "peer")
	await(t, b, "peer")
	await(t, b, "peer")
	await(t, c, "peer")
	if err := a.Send("through bob"); err != nil {
		t.Fatal(err)
	}
	if msg := await(t, c, "message"); msg.Name != "alice" || msg.Text != "through bob" {
		t.Fatalf("indirect peer received %+v", msg)
	}
}

func TestDisconnectUpdatesPeerStatus(t *testing.T) {
	a := newTestNode(t, "alice")
	b := newTestNode(t, "bob")
	if err := a.AddPeer(fmt.Sprintf("127.0.0.1:%d", b.Port)); err != nil {
		t.Fatal(err)
	}
	await(t, a, "peer")
	await(t, b, "peer")
	b.Close()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-a.Events:
			if event.Kind == "peer" && event.Status == "disconnected" {
				return
			}
		case <-timer.C:
			t.Fatal("peer disconnect was not reported")
		}
	}
}

func waitForMembers(t *testing.T, n *Node, others ...*Node) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		found := true
		for _, other := range others {
			member, ok := n.members[other.id]
			if !ok || member.name != other.Name || member.address != fmt.Sprintf("127.0.0.1:%d", other.Port) {
				found = false
				break
			}
		}
		n.mu.Unlock()
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not learn all group members", n.Name)
}

func TestLateJoinSharesMembersAcrossGroup(t *testing.T) {
	a := newTestNode(t, "alice")
	b := newTestNode(t, "bob")
	c := newTestNode(t, "charlie")
	d := newTestNode(t, "dana")
	for _, edge := range []struct{ from, to *Node }{{a, b}, {b, c}} {
		if err := edge.from.AddPeer(fmt.Sprintf("127.0.0.1:%d", edge.to.Port)); err != nil {
			t.Fatal(err)
		}
	}
	waitForMembers(t, a, b, c)
	waitForMembers(t, c, a, b)
	if err := d.AddPeer(fmt.Sprintf("127.0.0.1:%d", c.Port)); err != nil {
		t.Fatal(err)
	}
	waitForMembers(t, a, b, c, d)
	waitForMembers(t, b, a, c, d)
	waitForMembers(t, c, a, b, d)
	waitForMembers(t, d, a, b, c)

	// Visibility does not create new TCP connections across the group.
	a.mu.Lock()
	connections := len(a.peers)
	a.mu.Unlock()
	if connections != 1 {
		t.Fatalf("alice has %d direct connections; want 1", connections)
	}
	if err := d.Send("hi from dana"); err != nil {
		t.Fatal(err)
	}
	if msg := await(t, a, "message"); msg.Name != "dana" || msg.Text != "hi from dana" {
		t.Fatalf("alice received %+v", msg)
	}
}

func TestDepartedMemberExpires(t *testing.T) {
	a := newTestNode(t, "alice")
	b := newTestNode(t, "bob")
	if err := a.AddPeer(fmt.Sprintf("127.0.0.1:%d", b.Port)); err != nil {
		t.Fatal(err)
	}
	waitForMembers(t, a, b)
	b.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		connected := len(a.peers) != 0
		a.mu.Unlock()
		if !connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.expireMembers(time.Now().Add(memberTTL + time.Second))
	a.mu.Lock()
	_, present := a.members[b.id]
	a.mu.Unlock()
	if present {
		t.Fatal("departed member should have expired")
	}
	for {
		select {
		case event := <-a.Events:
			if event.Kind == "member" && event.ID == b.id && event.Status == "left" {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("member departure was not reported")
		}
	}
}

func TestIndirectMemberExpiresAfterLeaving(t *testing.T) {
	a := newTestNode(t, "alice")
	b := newTestNode(t, "bob")
	c := newTestNode(t, "charlie")
	if err := a.AddPeer(fmt.Sprintf("127.0.0.1:%d", b.Port)); err != nil {
		t.Fatal(err)
	}
	if err := b.AddPeer(fmt.Sprintf("127.0.0.1:%d", c.Port)); err != nil {
		t.Fatal(err)
	}
	waitForMembers(t, a, b, c)
	c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		connected := false
		for p := range b.peers {
			connected = connected || p.id == c.id
		}
		b.mu.Unlock()
		if !connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.expireMembers(time.Now().Add(memberTTL + time.Second))
	a.mu.Lock()
	_, present := a.members[c.id]
	a.mu.Unlock()
	if present {
		t.Fatal("indirect member should expire when they stop sending updates")
	}
}
