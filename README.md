# Crossfly

Crossfly is a Minecraft server that Java Edition and Bedrock Edition players can join at the same time,
natively. No Geyser, no Floodgate, no ViaProxy: Java players connect straight to the server and play in the
same world as Bedrock players. The Java crossplay experience is seamless.

It is built on [Dragonfly](https://github.com/df-mc/dragonfly), the Bedrock server written in Go, so it is
light: one small binary, low memory, starts in seconds.

## Versions

- Java Edition 26.3 and 26.2
- Bedrock Edition 1.26.60

Put [Velocity](https://papermc.io/software/velocity) with [ViaVersion](https://viaversion.com) in front of
Crossfly and Java players on many more versions can join too.

## Running it

1. Download the build for your system from [Releases](https://github.com/ezchr/crossfly/releases).
2. Run it once. It creates `config.toml` and a `world` folder, and starts listening:
   Bedrock players on port 19132 (UDP), Java players on port 25565 (TCP).
3. Stop it with Ctrl+C, change `config.toml` to your liking, and start it again.

### Java settings

The `[Java]` section of `config.toml`:

| Setting | Default | What it does |
|---|---|---|
| `Enabled` | `true` | Lets Java players join. |
| `Address` | `":25565"` | Address and port for Java players. |
| `OnlineMode` | `true` | Only Microsoft accounts can join. Turn it off for offline accounts. |
| `VelocitySecretFile` | `""` | Path to Velocity's `forwarding.secret`. Set it when the server sits behind Velocity with modern forwarding; Velocity then checks the accounts. |
| `CompressionThreshold` | `256` | Packets this big or bigger are compressed. Use `-1` when only Velocity on the same machine connects. |
| `ChunkRadius` | `12` | Largest view distance Java players get. |

Everything else in `config.toml` is Dragonfly's own configuration.

## Building from source

You need Go 1.26 or newer.

```
git clone https://github.com/ezchr/crossfly
cd crossfly
go build -o crossfly .
```

The Java side lives in [server/java](server/java); its README explains how it works and how to use it in your
own Go program.

## Credits

Crossfly is a fork of [Dragonfly](https://github.com/df-mc/dragonfly) by Dragonfly Tech and its contributors,
who wrote the server Crossfly runs on. It is not an official df-mc project, so please report Crossfly problems
here rather than to Dragonfly. Fixes that belong in Dragonfly itself are sent upstream.

## License

MIT, like Dragonfly. See [LICENSE](LICENSE).
