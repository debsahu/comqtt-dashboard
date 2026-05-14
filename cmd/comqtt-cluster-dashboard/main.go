// Package main is the cluster-mode comqtt broker with the dashboard add-on
// pre-wired. Drop-in replacement for upstream `comqtt-cluster` on the same
// flags. Cluster transport, Raft, and gossip discovery are unchanged; the
// dashboard mounts on the existing :8080 HTTP listener alongside
// /api/v1/cluster/* and /api/v1/mqtt/*.
//
// Structure mirrors upstream cmd/cluster/main.go (MIT, wind-c/comqtt) with
// the dashboard wiring borrowed from cmd/comqtt-dashboard. The two
// dashboard binaries diverge only in the cluster bits: agent init, the
// cluster REST handler set, and the redis client that feeds the dashboard
// event bridge.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	rv8 "github.com/redis/go-redis/v9"
	cs "github.com/wind-c/comqtt/v2/cluster"
	"github.com/wind-c/comqtt/v2/cluster/log"
	csRt "github.com/wind-c/comqtt/v2/cluster/rest"
	coredis "github.com/wind-c/comqtt/v2/cluster/storage/redis"
	"github.com/wind-c/comqtt/v2/config"
	"github.com/wind-c/comqtt/v2/mqtt"
	"github.com/wind-c/comqtt/v2/mqtt/hooks/auth"
	"github.com/wind-c/comqtt/v2/mqtt/listeners"
	mqttRt "github.com/wind-c/comqtt/v2/mqtt/rest"
	"github.com/wind-c/comqtt/v2/plugin"
	hauth "github.com/wind-c/comqtt/v2/plugin/auth/http"
	mauth "github.com/wind-c/comqtt/v2/plugin/auth/mysql"
	pauth "github.com/wind-c/comqtt/v2/plugin/auth/postgresql"
	rauth "github.com/wind-c/comqtt/v2/plugin/auth/redis"
	cokafka "github.com/wind-c/comqtt/v2/plugin/bridge/kafka"

	addonclusterrest "github.com/debsahu/comqtt-dashboard/cluster/rest"
	"github.com/debsahu/comqtt-dashboard/dashboard"
	"github.com/debsahu/comqtt-dashboard/internal/comqttauthadapter"
	"github.com/debsahu/comqtt-dashboard/mqttauth"
	addonrest "github.com/debsahu/comqtt-dashboard/rest"
	"github.com/debsahu/comqttauth"
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
	var confFile, members string
	cfg := config.New()

	flag.StringVar(&confFile, "conf", "", "read the program parameters from the config file")
	flag.UintVar(&cfg.StorageWay, "storage-way", 3, "storage way: 0 memory, 1 bolt, 2 badger, 3 redis (cluster mode requires redis)")
	flag.UintVar(&cfg.Auth.Way, "auth-way", 0, "authentication way: 0 anonymous, 1 username/password, 2 clientid")
	flag.UintVar(&cfg.Auth.Datasource, "auth-ds", 0, "authentication datasource: 0 free, 1 redis, 2 mysql, 3 postgresql, 4 http")
	flag.StringVar(&cfg.Auth.ConfPath, "auth-path", "", "config file path corresponding to the auth-datasource")
	var authRegexEnabled bool
	var authRegexStrict bool
	flag.BoolVar(&authRegexEnabled, "auth-regex", false, "enable regex authorization layer (requires --auth-way>0 and --auth-ds!=4)")
	flag.BoolVar(&authRegexStrict, "auth-regex-strict", false, "first-load seed mode: false=allow-all (default), true=deny-all (operator must whitelist via explicit allow rules)")
	flag.StringVar(&cfg.Mqtt.TCP, "tcp", ":1883", "network address for MQTT TCP listener")
	flag.StringVar(&cfg.Mqtt.WS, "ws", ":1882", "network address for MQTT WebSocket listener")
	flag.StringVar(&cfg.Mqtt.HTTP, "http", ":8080", "network address for HTTP listener (REST + dashboard)")
	flag.StringVar(&cfg.Cluster.NodeName, "node-name", "", "node name; must be unique in the cluster")
	flag.StringVar(&cfg.Cluster.BindAddr, "bind-ip", "127.0.0.1", "ip for discovery and inter-node communication (intranet addr)")
	flag.IntVar(&cfg.Cluster.BindPort, "gossip-port", 7946, "port used for cluster node discovery")
	flag.IntVar(&cfg.Cluster.RaftPort, "raft-port", 8946, "port used for raft peer communication")
	flag.BoolVar(&cfg.Cluster.RaftBootstrap, "raft-bootstrap", false, "true for the first cluster node; elects a leader without peers present")
	flag.StringVar(&cfg.Cluster.RaftLogLevel, "raft-log-level", "error", "raft log level: debug, info, warn, error")
	flag.StringVar(&members, "members", "", "comma-separated seed members, e.g. 192.168.0.103:7946,192.168.0.104:7946")
	flag.BoolVar(&cfg.Cluster.GrpcEnable, "grpc-enable", false, "use grpc for raft transport and inter-node communication")
	flag.IntVar(&cfg.Cluster.GrpcPort, "grpc-port", 17946, "grpc communication port between nodes")
	flag.StringVar(&cfg.Redis.Options.Addr, "redis", "127.0.0.1:6379", "redis address (cluster storage + dashboard event bridge)")
	flag.StringVar(&cfg.Redis.Options.Password, "redis-pass", "", "redis password")
	flag.IntVar(&cfg.Redis.Options.DB, "redis-db", 0, "redis db")
	flag.BoolVar(&cfg.Log.Enable, "log-enable", true, "log enabled or not")
	flag.StringVar(&cfg.Log.Filename, "log-file", "./logs/comqtt.log", "log filename")
	flag.StringVar(&cfg.Cluster.NodesFileDir, "nodes-file-dir", "", "directory holds nodes.json assisting node discovery")
	flag.Parse()

	if len(confFile) > 0 {
		if cfg, err = config.Load(confFile); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	} else if members != "" {
		cfg.Cluster.Members = strings.Split(members, ",")
	} else {
		cfg.Cluster.Members = []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Cluster.BindPort))}
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
	log.Info("comqtt-cluster-dashboard initializing...")
	if err := initStorage(server, cfg); err != nil {
		return err
	}
	if err := initAuth(server, cfg); err != nil {
		return err
	}
	if err := initBridge(server, cfg); err != nil {
		return err
	}

	if cfg.Cluster.Members == nil {
		return fmt.Errorf("cluster: %w (members parameter etc)", config.ErrClusterOpts)
	}
	agent := cs.NewAgent(&cfg.Cluster)
	agent.BindMqttServer(server)
	if err := agent.Start(); err != nil {
		return fmt.Errorf("cluster start: %w", err)
	}
	log.Info("cluster node created")

	var listenerConfig *listeners.Config
	if tlsConfig, err := config.GenTlsConfig(cfg); err != nil {
		return fmt.Errorf("tls: %w", err)
	} else if tlsConfig != nil {
		listenerConfig = &listeners.Config{TLSConfig: tlsConfig}
	}
	if err := server.AddListener(listeners.NewTCP("tcp", cfg.Mqtt.TCP, listenerConfig)); err != nil {
		return fmt.Errorf("add tcp listener: %w", err)
	}
	if err := server.AddListener(listeners.NewWebsocket("ws", cfg.Mqtt.WS, listenerConfig)); err != nil {
		return fmt.Errorf("add websocket listener: %w", err)
	}

	// The cluster broker uses redis for storage; reuse those credentials for
	// the dashboard event bridge so both speak to the same redis instance.
	// Hub/Bridge pub-sub is namespaced separately from cluster storage keys.
	redisClient := rv8.NewClient(&rv8.Options{
		Addr:     cfg.Redis.Options.Addr,
		DB:       cfg.Redis.Options.DB,
		Password: cfg.Redis.Options.Password,
	})
	defer redisClient.Close()

	// Compose REST handlers: upstream cluster + upstream MQTT + dashboard
	// addon endpoints (per-node) + dashboard cluster mirrors (fan-out) +
	// the dashboard UI itself.
	handlers := csRt.New(agent).GenHandlers()
	for path, h := range mqttRt.New(server).GenHandlers() {
		handlers[path] = h
	}
	for path, h := range addonrest.New(server).GenHandlers() {
		handlers[path] = h
	}
	for path, h := range addonclusterrest.New(agent, cfg.Mqtt.HTTP).GenHandlers() {
		handlers[path] = h
	}

	// MQTT auth-management backend, same as the single-mode binary. nil
	// when broker auth is anonymous or HTTP-delegated.
	mqttAuthCfg, err := mqttauth.FromComqttConfig(cfg)
	if err != nil {
		return fmt.Errorf("mqtt auth config: %w", err)
	}
	var mqttAuthBackend mqttauth.Backend
	if mqttAuthCfg != nil {
		mqttAuthBackend, err = mqttauth.New(*mqttAuthCfg)
		if err != nil {
			return fmt.Errorf("mqtt auth backend: %w", err)
		}
		defer mqttAuthBackend.Close()
	}

	// Optional regex authorization layer. When --auth-regex=true the cmd
	// binary additionally constructs a comqttauth.Backend, installs a
	// comqttauth.Hook in the broker alongside the upstream auth plugin
	// (coexist mode), and wires the Backend into the dashboard so the
	// regex page can manage rules. The Hook is regex-ACL-only; upstream
	// plugin/auth/* keeps handling connection auth + exact-match ACL.
	var regexBackend comqttauth.Backend
	if authRegexEnabled {
		regexCfg, err := comqttauthadapter.FromComqttConfig(cfg)
		if err != nil {
			return fmt.Errorf("comqttauth config: %w", err)
		}
		if regexCfg == nil {
			log.Warn("--auth-regex=true ignored: anonymous or HTTP-delegated auth has no manageable backend")
		} else {
			regexBackend, err = comqttauth.New(*regexCfg)
			if err != nil {
				return fmt.Errorf("comqttauth backend: %w", err)
			}
			defer regexBackend.Close()
			if err := server.AddHook(&comqttauth.Hook{}, &comqttauth.HookOptions{Backend: regexBackend}); err != nil {
				return fmt.Errorf("comqttauth hook: %w", err)
			}
			seedCtx, cancelSeed := context.WithTimeout(context.Background(), 5*time.Second)
			seeded, err := regexBackend.GetRegexSeeded(seedCtx)
			cancelSeed()
			if err != nil {
				return fmt.Errorf("comqttauth seeded check: %w", err)
			}
			if !seeded {
				seed := comqttauth.RegexRule{
					Order:         999999,
					Permission:    comqttauth.PermissionAllow,
					SubjectKind:   comqttauth.SubjectUsername,
					Action:        comqttauth.ActionAll,
					TopicPatterns: []string{"#"},
				}
				if authRegexStrict {
					seed.Permission = comqttauth.PermissionDeny
				}
				putCtx, cancelPut := context.WithTimeout(context.Background(), 5*time.Second)
				if _, err := regexBackend.PutRegexRule(putCtx, seed); err != nil {
					cancelPut()
					return fmt.Errorf("comqttauth seed rule: %w", err)
				}
				cancelPut()
				markCtx, cancelMark := context.WithTimeout(context.Background(), 5*time.Second)
				if err := regexBackend.SetRegexSeeded(markCtx); err != nil {
					cancelMark()
					return fmt.Errorf("comqttauth mark seeded: %w", err)
				}
				cancelMark()
				log.Info("seeded initial regex rule",
					"permission", seed.Permission.String(),
					"strict_mode", authRegexStrict)
			}
		}
	}

	dashCleanup := func() {}
	if dashCfg.Enabled {
		dashRoutes, cleanup, err := dashboard.Routes(dashboard.Options{
			Server:             server,
			Cluster:            true,
			ClusterAgent:       agent,
			Node:               cfg.Cluster.NodeName,
			Secret:             dashCfg.decodeSecret(),
			PasswordExpiryDays: dashCfg.PasswordExpiryDays,
			Redis:              redisClient,
			MQTTAuth:           mqttAuthBackend,
			RegexBackend:       regexBackend,
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
	log.Info("cluster node started")

	select {
	case err := <-errCh:
		dashCleanup()
		agent.Stop()
		server.Close()
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
		log.Warn("caught signal, stopping...")
	}
	dashCleanup()
	agent.Stop()
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
	ledger := auth.Ledger{}
	if conf.Auth.BlacklistPath != "" {
		if err := plugin.LoadYaml(conf.Auth.BlacklistPath, &ledger); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
	}
	switch conf.Auth.Datasource {
	case config.AuthDSFree:
		data, err := os.ReadFile(conf.Auth.ConfPath)
		if err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		return server.AddHook(new(auth.Hook), &auth.Options{Data: data})
	case config.AuthDSRedis:
		opts := rauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		if err := server.AddHook(new(rauth.Auth), &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		opts.SetBlacklist(&ledger)
	case config.AuthDSMysql:
		opts := mauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		if err := server.AddHook(new(mauth.Auth), &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		opts.SetBlacklist(&ledger)
	case config.AuthDSPostgresql:
		opts := pauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		if err := server.AddHook(new(pauth.Auth), &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		opts.SetBlacklist(&ledger)
	case config.AuthDSHttp:
		opts := hauth.Options{}
		if err := plugin.LoadYaml(conf.Auth.ConfPath, &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		if err := server.AddHook(new(hauth.Auth), &opts); err != nil {
			return fmt.Errorf("%s: %w", logMsg, err)
		}
		opts.SetBlacklist(&ledger)
	}
	return nil
}

func initStorage(server *mqtt.Server, conf *config.Config) error {
	if conf.StorageWay != config.StorageWayRedis {
		return config.ErrStorageWay
	}
	return server.AddHook(new(coredis.Storage), &coredis.Options{
		HPrefix: conf.Redis.HPrefix,
		Options: &rv8.Options{
			Addr:     conf.Redis.Options.Addr,
			DB:       conf.Redis.Options.DB,
			Username: conf.Redis.Options.Username,
			Password: conf.Redis.Options.Password,
		},
	})
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
