// Package main is the single-mode comqtt broker with the dashboard add-on
// pre-wired. Drop-in replacement for `comqtt-single` on the same flags.
//
// This binary is intentionally a thin wrapper: stock comqtt construction,
// then dashboard.Routes mounted on the existing :8080 listener alongside
// /api/v1/*. The dashboard package itself lives in
// github.com/debsahu/comqtt-dashboard/dashboard and can be embedded into
// any other broker driver in the same way.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	rv8 "github.com/redis/go-redis/v9"
	"github.com/wind-c/comqtt/v2/cluster/log"
	"github.com/wind-c/comqtt/v2/config"
	"github.com/wind-c/comqtt/v2/mqtt"
	"github.com/wind-c/comqtt/v2/mqtt/hooks/auth"
	"github.com/wind-c/comqtt/v2/mqtt/hooks/storage/badger"
	"github.com/wind-c/comqtt/v2/mqtt/hooks/storage/bolt"
	"github.com/wind-c/comqtt/v2/mqtt/hooks/storage/redis"
	"github.com/wind-c/comqtt/v2/mqtt/listeners"
	upstreamrest "github.com/wind-c/comqtt/v2/mqtt/rest"
	"github.com/wind-c/comqtt/v2/plugin"
	hauth "github.com/wind-c/comqtt/v2/plugin/auth/http"
	mauth "github.com/wind-c/comqtt/v2/plugin/auth/mysql"
	pauth "github.com/wind-c/comqtt/v2/plugin/auth/postgresql"
	rauth "github.com/wind-c/comqtt/v2/plugin/auth/redis"
	cokafka "github.com/wind-c/comqtt/v2/plugin/bridge/kafka"
	"go.etcd.io/bbolt"

	"github.com/debsahu/comqtt-dashboard/dashboard"
	addonrest "github.com/debsahu/comqtt-dashboard/rest"
)

func pprof() {
	go func() {
		log.Info("listen pprof", "error", http.ListenAndServe(":6060", nil))
	}()
}

func main() {
	sigCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := realMain(sigCtx); err != nil {
		log.Error("main", "error", err)
		os.Exit(1)
	}
}

func realMain(ctx context.Context) error {
	var err error
	var confFile string
	cfg := config.New()

	flag.StringVar(&confFile, "conf", "", "read the program parameters from the config file")
	flag.UintVar(&cfg.StorageWay, "storage-way", 1, "storage way: 0 memory, 1 bolt, 2 badger, 3 redis")
	flag.UintVar(&cfg.Auth.Way, "auth-way", 0, "authentication way: 0 anonymous, 1 username/password, 2 clientid")
	flag.UintVar(&cfg.Auth.Datasource, "auth-ds", 0, "authentication datasource: 0 free, 1 redis, 2 mysql, 3 postgresql, 4 http")
	flag.StringVar(&cfg.Auth.ConfPath, "auth-path", "", "config file path corresponding to the auth-datasource")
	flag.StringVar(&cfg.Mqtt.TCP, "tcp", ":1883", "network address for MQTT TCP listener")
	flag.StringVar(&cfg.Mqtt.WS, "ws", ":1882", "network address for MQTT WebSocket listener")
	flag.StringVar(&cfg.Mqtt.QUIC, "quic", ":2000", "network address for MQTT QUIC listener")
	flag.StringVar(&cfg.Mqtt.HTTP, "http", ":8080", "network address for HTTP listener (REST + dashboard)")
	flag.BoolVar(&cfg.Log.Enable, "log-enable", true, "log enabled or not")
	flag.StringVar(&cfg.Log.Filename, "log-file", "./logs/comqtt.log", "log filename")
	flag.Parse()

	if len(confFile) > 0 {
		if cfg, err = config.Load(confFile); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}
	dashCfg, err := loadAddonConfig(confFile)
	if err != nil {
		return err
	}

	if cfg.PprofEnable {
		pprof()
	}

	log.Init(&cfg.Log)
	if cfg.Log.Enable && cfg.Log.Output == log.OutputFile {
		fmt.Println("log output to the files, please check")
	}

	cfg.Mqtt.Options.Logger = log.Default()
	server := mqtt.New(&cfg.Mqtt.Options)
	log.Info("comqtt-dashboard initializing...")
	if err := initStorage(server, cfg); err != nil {
		return err
	}
	if err := initAuth(server, cfg); err != nil {
		return err
	}
	if err := initBridge(server, cfg); err != nil {
		return err
	}

	var listenerConfig, listenerQuicConfig *listeners.Config
	if tlsConfig, err := config.GenTlsConfig(cfg); err != nil {
		return fmt.Errorf("tls: %w", err)
	} else if tlsConfig != nil {
		listenerConfig = &listeners.Config{TLSConfig: tlsConfig, ZeroRTT: cfg.Mqtt.Tls.ZeroRTT}
		listenerQuicConfig = listenerConfig
	}

	if err := server.AddListener(listeners.NewTCP("tcp", cfg.Mqtt.TCP, listenerConfig)); err != nil {
		return fmt.Errorf("add tcp listener: %w", err)
	}

	if listenerConfig == nil {
		tlsConfig, err := config.GenerateSelfSignedCert()
		if err != nil {
			return fmt.Errorf("self-signed cert: %w", err)
		}
		listenerQuicConfig = &listeners.Config{TLSConfig: tlsConfig, ZeroRTT: cfg.Mqtt.Tls.ZeroRTT}
	}
	if err := server.AddListener(listeners.NewQUIC("quic", cfg.Mqtt.QUIC, listenerQuicConfig)); err != nil {
		return fmt.Errorf("add quic listener: %w", err)
	}
	if err := server.AddListener(listeners.NewWebsocket("ws", cfg.Mqtt.WS, listenerConfig)); err != nil {
		return fmt.Errorf("add websocket listener: %w", err)
	}

	// Compose REST handlers: upstream stock + dashboard-specific endpoints
	// + dashboard UI routes. Order matters only for overlapping keys; there
	// are none here.
	handlers := upstreamrest.New(server).GenHandlers()
	for path, h := range addonrest.New(server).GenHandlers() {
		handlers[path] = h
	}

	dashCleanup := func() {}
	if dashCfg.Enabled {
		dashRoutes, cleanup, err := dashboard.Routes(dashboard.Options{
			Server:             server,
			Cluster:            false,
			Secret:             dashCfg.decodeSecret(),
			PasswordExpiryDays: dashCfg.PasswordExpiryDays,
		})
		if err != nil {
			return fmt.Errorf("dashboard routes: %w", err)
		}
		dashCleanup = cleanup
		for path, h := range dashRoutes {
			handlers[path] = h
		}
	}

	if err := server.AddListener(listeners.NewHTTP("stats", cfg.Mqtt.HTTP, nil, handlers)); err != nil {
		return fmt.Errorf("add http listener: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := server.Serve(); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		dashCleanup()
		server.Close()
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
		log.Warn("caught signal, stopping...")
	}
	dashCleanup()
	server.Close()
	log.Info("shutdown complete")
	return nil
}

func initAuth(server *mqtt.Server, conf *config.Config) error {
	const logMsg = "init auth"
	if conf.Auth.Way == config.AuthModeAnonymous {
		return server.AddHook(new(auth.AllowHook), nil)
	}
	if conf.Auth.Way != config.AuthModeUsername && conf.Auth.Way != config.AuthModeClientid {
		return config.ErrAuthWay
	}
	switch conf.Auth.Datasource {
	case config.AuthDSRedis:
		opts := rauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		return server.AddHook(new(rauth.Auth), &opts)
	case config.AuthDSMysql:
		opts := mauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		return server.AddHook(new(mauth.Auth), &opts)
	case config.AuthDSPostgresql:
		opts := pauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		return server.AddHook(new(pauth.Auth), &opts)
	case config.AuthDSHttp:
		opts := hauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		return server.AddHook(new(hauth.Auth), &opts)
	}
	return nil
}

func initStorage(server *mqtt.Server, conf *config.Config) error {
	switch conf.StorageWay {
	case config.StorageWayBolt:
		return server.AddHook(new(bolt.Hook), &bolt.Options{
			Path:    conf.StoragePath,
			Options: &bbolt.Options{Timeout: 500 * time.Millisecond},
		})
	case config.StorageWayBadger:
		return server.AddHook(new(badger.Hook), &badger.Options{Path: conf.StoragePath})
	case config.StorageWayRedis:
		return server.AddHook(new(redis.Hook), &redis.Options{
			HPrefix: conf.Redis.HPrefix,
			Options: &rv8.Options{
				Addr:     conf.Redis.Options.Addr,
				DB:       conf.Redis.Options.DB,
				Password: conf.Redis.Options.Password,
			},
		})
	}
	return nil
}

func initBridge(server *mqtt.Server, conf *config.Config) error {
	if conf.BridgeWay == config.BridgeWayNone {
		return nil
	}
	if conf.BridgeWay == config.BridgeWayKafka {
		opts := cokafka.Options{}
		if err := plugin.LoadYaml(conf.BridgePath, &opts); err != nil {
			return fmt.Errorf("init bridge: %w", err)
		}
		return server.AddHook(new(cokafka.Bridge), &opts)
	}
	return nil
}
