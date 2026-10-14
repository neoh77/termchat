# termchat

A single-room, peer-to-peer terminal chat. One Go binary runs the TUI and the network listener; there is no separate server.

## Build

Requires Go 1.24 or newer:

```sh
CGO_ENABLED=0 go build -trimpath -o termchat .
```

Copy the resulting `termchat` binary to each Linux machine. No Go installation is needed to run the binary.

Each commit pushed to `main` also creates a [GitHub release](https://github.com/neoh77/termchat/releases) with a statically linked Linux amd64 binary named `termchat`. Releases use SemVer tags: the first is `v0.1.0`, then each new release increments the patch version (`v0.1.1`, `v0.1.2`, …). Existing `build-<commit SHA>` releases do not affect this sequence. To start a new major or minor series, create a higher SemVer tag; subsequent releases increment its patch version.

## Chat

```sh
# First machine (listens on TCP 8085 if available)
./termchat --name alice

# Another machine (replace the address with Alice's reachable IP)
./termchat --name bob --peer 192.168.1.10
```

Messages travel both ways once connected. To add someone while running, press **Ctrl+A**, type their IP (or `IP:port`), and press **Enter**. Peers in a connected group forward messages to one another, so every member doesn't have to enter every other member's address.

The TUI shows your local `10.*` or `192.*` IPv4 address and actual listening port to share with other members. If multiple matching interfaces exist, it lists their addresses; choose one reachable by your peers.

| Key | Action |
| --- | --- |
| Enter | Send a message, or connect to the entered peer |
| Ctrl+A | Enter a peer IP address |
| Esc | Cancel peer entry |
| Ctrl+Q | Quit |

Use `--peer` more than once to connect to several addresses on startup. The Peers panel shows connection status, including incoming peers. Connections that fail can be retried with Ctrl+A.

## Ports and connectivity

By default, termchat attempts to listen on **TCP port 8085**. If that port cannot be bound (for example, it is occupied), it attempts **8080**. The TUI header always shows the actual listening port. You can choose one explicitly with `--port 8080`; an explicit port does not fall back. An IP entered without a port means port 8085, so include `:8080` when connecting to a machine that fell back.

Each member must be reachable from at least one other member over its listening TCP port. For machines behind NAT, configure port forwarding or use a network where their IPs are directly reachable. This is a plaintext chat intended for trusted networks; it does not authenticate or encrypt peers. Peer addresses and messages are not saved between runs.
