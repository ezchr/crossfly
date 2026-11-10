package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/java/javasession"
	jserver "github.com/df-mc/dragonfly/server/java/protocol/server"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/pelletier/go-toml"
)

// fileConfig is config.toml: Dragonfly's settings plus the Java Edition listener and, in Crossfly Full, its
// features: the commands, the flat world, mobs, Java redstone and capes (Edition: core.go, full.go).
type fileConfig struct {
	server.UserConfig
	Java javaConfig
	Edition
}

// javaConfig holds the settings of the Java Edition listener.
type javaConfig struct {
	// Enabled starts the Java Edition listener, so Java Edition players can join next to Bedrock players.
	Enabled bool
	// Address is the TCP address Java Edition players connect to, including the port.
	Address string
	// OnlineMode checks every Java login with Mojang, so only Microsoft accounts get in. Turn it off
	// behind Velocity (VelocitySecretFile) or to allow offline accounts.
	OnlineMode bool
	// VelocitySecretFile is the path to Velocity's forwarding.secret file. When set, the server only
	// takes players through Velocity's modern forwarding, and OnlineMode is ignored (Velocity checks
	// the accounts). Leave it empty when players connect directly.
	VelocitySecretFile string
	// CompressionThreshold compresses packets of at least this many bytes. -1 turns compression off,
	// which suits a server that only Velocity on the same machine connects to.
	CompressionThreshold int
	// ChunkRadius is the largest view distance in chunks a Java player gets. Their own setting is
	// used when it is smaller.
	ChunkRadius int
}

func defaultConfig() fileConfig {
	return fileConfig{
		UserConfig: server.DefaultConfig(),
		Java: javaConfig{
			Enabled:              true,
			Address:              ":25565",
			OnlineMode:           true,
			CompressionThreshold: 256,
			ChunkRadius:          12,
		},
		Edition: defaultEdition(),
	}
}

func main() {
	log := slog.Default()
	chat.Global.Subscribe(chat.StdoutSubscriber{})
	fc, created, err := readConfig()
	if err != nil {
		log.Error("config.toml", "err", err)
		os.Exit(1)
	}
	// Blocks, items and entities of Crossfly Full are registered before the server is configured: the block
	// registry and the creative inventory are made then.
	if err := fc.Edition.register(log); err != nil {
		log.Error("config.toml", "err", err)
		os.Exit(1)
	}
	conf, err := fc.UserConfig.Config(log)
	if err != nil {
		log.Error("config.toml", "err", err)
		os.Exit(1)
	}
	conf.ShutdownMessage = shutdownTranslation
	if err := fc.Edition.configure(&conf); err != nil {
		log.Error("config.toml", "err", err)
		os.Exit(1)
	}

	srv := conf.New()
	// Ctrl+C (SIGINT) or SIGTERM stops the server, saving the worlds.
	srv.CloseOnProgramEnd()
	fc.Edition.start(srv, log)
	if created {
		firstStart(log)
	}
	if fc.Java.Enabled {
		if err := startJava(srv, conf, fc.Java, log); err != nil {
			log.Error("Java Edition listener", "err", err)
			os.Exit(1)
		}
	}

	srv.Listen()
	for p := range srv.Accept() {
		_ = p
	}
}

// startJava starts the Java Edition listener and lets its players join srv.
func startJava(srv *server.Server, conf server.Config, jc javaConfig, log *slog.Logger) error {
	var secret []byte
	if jc.VelocitySecretFile != "" {
		b, err := os.ReadFile(jc.VelocitySecretFile)
		if err != nil {
			return fmt.Errorf("read Velocity secret: %w", err)
		}
		if secret = bytes.TrimSpace(b); len(secret) == 0 {
			return fmt.Errorf("Velocity secret file %s is empty", jc.VelocitySecretFile)
		}
	}
	l, err := jserver.Listen(jc.Address, jserver.Config{
		OnlineMode:           jc.OnlineMode && secret == nil,
		VelocitySecret:       secret,
		CompressionThreshold: jc.CompressionThreshold,
		Log:                  log,
		Status: func() jserver.Status {
			return jserver.Status{MOTD: conf.Name, MaxPlayers: srv.MaxPlayerCount(), Online: srv.PlayerCount()}
		},
	})
	if err != nil {
		return err
	}
	log.Info("Java Edition listener running.", "addr", l.Addr(), "online-mode", jc.OnlineMode && secret == nil, "velocity", secret != nil)
	go javasession.Run(javasession.Config{
		Server:      srv,
		Listener:    l,
		ChunkRadius: jc.ChunkRadius,
		Log:         log,
		JoinMessage: conf.JoinMessage,
		QuitMessage: conf.QuitMessage,
	})
	return nil
}

// readConfig reads config.toml, or writes the default one if it does not exist yet (created is then true).
// Settings missing from an older file keep their defaults.
func readConfig() (c fileConfig, created bool, err error) {
	c = defaultConfig()
	data, err := os.ReadFile("config.toml")
	if os.IsNotExist(err) {
		data, err := toml.Marshal(c)
		if err != nil {
			return c, false, fmt.Errorf("encode default config: %w", err)
		}
		return c, true, os.WriteFile("config.toml", data, 0644)
	}
	if err != nil {
		return c, false, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		return c, false, fmt.Errorf("decode config: %w", err)
	}
	return c, false, nil
}
