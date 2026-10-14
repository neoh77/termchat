# termchat

A single-room, peer-to-peer terminal chat. One Go binary runs the TUI and the network listener; there is no separate server.

## Build

Requires Go 1.24 or newer:

```sh
CGO_ENABLED=0 go build -trimpath -o termchat .
```

Copy the resulting `termchat` binary to each Linux machine. No Go installation is needed to run the binary.

Each commit pushed to `main` also creates a [GitHub release](https://github.com/neoh77/termchat/releases) with a statically linked Linux amd64 binary named `termchat`. Releases use SemVer tags: the first is `v0.1.0`, then each new release increments the patch version (`v0.1.1`, `v0.1.2`, …). Existing `build-<commit SHA>` releases do not affect this sequence. To start a new major or minor series, create a higher SemVer tag; subsequent releases increment its patch version.

The status bar shows the release version (for example, `v0.1.2`). Binaries built locally with the command above show `dev` instead.

## Chat

```sh
# First machine (listens on TCP 8085 if available)
./termchat --name alice

# Another machine (replace the address with Alice's reachable IP)
./termchat --name bob --peer 192.168.1.10
```

Messages travel both ways once connected. To add someone while running, press **Ctrl+A**, type their IP (or `IP:port`), and press **Enter**. Members share their nicknames and IP:port addresses with the whole connected group, so everyone sees new arrivals. A member is marked **direct** when you have a TCP connection to them and **via group** when their messages are forwarded through another member. Members who leave disappear after approximately 20 seconds without an update. Address sharing does not automatically open new connections.

Connections you initiate (with `--peer` or Ctrl+A) automatically retry every few seconds for up to 60 seconds after a disconnect or an initial connection failure. The Members panel shows **reconnecting** while retrying and a failure if the window expires; you can then try again with Ctrl+A. Connections that were only accepted from another member do not dial back. A member may briefly disappear during a longer restart, and messages sent while disconnected are not replayed.

Type `/me waves` to send an action, displayed to everyone as `* alice waves` (using your nickname). Type `/update` to check the latest GitHub release and install its `termchat` binary if it is newer; restart termchat to use the update. This manual update requires internet access and a writable installation directory and is available for Linux amd64. Locally built `dev` binaries install the latest release when `/update` is used. The current binary is kept if the download or installation fails.

New messages or actions from other members show a high-contrast banner in place of the shortcut hints, as well as a count in the Chat pane title. Typing or using a shortcut clears the alert; messages you send do not add to it.

The TUI shows your local `10.*` or `192.*` IPv4 address and actual listening port to share with other members. If multiple matching interfaces exist, it lists their addresses; choose one reachable by your peers.

| Key | Action |
| --- | --- |
| Enter | Send a message or `/me` action, run `/update`, or connect to the entered peer |
| Ctrl+A | Enter a peer IP address |
| PgUp / PgDn | Scroll through members |
| Esc | Cancel peer entry |
| Ctrl+Q | Quit |

Use `--peer` more than once to connect to several addresses on startup. The Members panel shows participants across the group and connection failures. Run the same version of termchat on each machine so membership updates use the same protocol.

## Ports and connectivity

By default, termchat attempts to listen on **TCP port 8085**. If that port cannot be bound (for example, it is occupied), it attempts **8080**. The TUI header always shows the actual listening port. You can choose one explicitly with `--port 8080`; an explicit port does not fall back. An IP entered without a port means port 8085, so include `:8080` when connecting to a machine that fell back.

Each member must be reachable from at least one other member over its listening TCP port. For machines behind NAT, configure port forwarding or use a network where their IPs are directly reachable. This is a plaintext chat intended for trusted networks; it does not authenticate or encrypt peers. Peer addresses and messages are not saved between runs.
