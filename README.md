# Crossfly

Crossfly is a Minecraft server where **Java Edition and Bedrock Edition players play together natively**.
It is built on [Dragonfly](https://github.com/df-mc/dragonfly), the Bedrock server written in Go, and adds a
Java Edition layer written in Go inside the server: Java players connect straight to it, with no Geyser,
ViaProxy or ViaBedrock in between, and share the same world with Bedrock players.

Like Dragonfly, Crossfly is used as a library: you write your server in Go and import it.

## What you get

- **Both editions on one server.** Bedrock players join as they would on any Dragonfly server; Java players
  join the Java listener. Everyone sees each other, chats, fights and builds in the same world.
- **No translation proxy.** The Java protocol is spoken in Go inside the server and renders the server's own
  world, blocks, items, entities, inventories and forms (as Java dialogs) directly.
- **Light.** One Go binary, low memory, no JVM.
- **Behind Velocity or standalone.** Velocity modern forwarding, or the Java listener on its own with online
  mode (Microsoft accounts) or offline mode.
- **Skins across editions.** Bedrock players' skins show for Java players and Java skins show for Bedrock players.

Supported versions: Java Edition **26.3** and **26.2**, Bedrock Edition **1.26.60**.

## Getting started

Crossfly keeps Dragonfly's module path, so existing Dragonfly servers switch with one line in `go.mod`:

```
replace github.com/df-mc/dragonfly => github.com/ezchr/crossfly main
```

then `go mod tidy` (it turns `main`, or a release tag, into a version). Your Dragonfly code keeps working as is.
Then start the Java listener next to your Bedrock one; see [server/java/README.md](server/java/README.md) for
the full setup, options and Velocity forwarding.

Requires **Go 1.26** or newer.

## Relationship with Dragonfly

Crossfly is a fork of Dragonfly that follows it closely: upstream changes are merged in regularly, and fixes
that belong in Dragonfly itself are sent upstream. For Dragonfly's API, documentation and guides, see the
[Dragonfly repository](https://github.com/df-mc/dragonfly) and its [wiki](https://github.com/df-mc/dragonfly/wiki).
Crossfly is not an official df-mc project; please report Crossfly issues here, not to Dragonfly.

## License

MIT, like Dragonfly: see [LICENSE](LICENSE). Dragonfly is copyright Dragonfly Tech and its contributors.
