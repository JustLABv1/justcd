package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/justlab/justcd/services/backend/internal/clusteragent"
	"k8s.io/client-go/rest"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	path := flag.String("config", "/etc/justcd-agent/config.json", "local agent configuration")
	flag.Parse()
	data, err := os.ReadFile(*path)
	if err != nil {
		slog.Error("could not read agent configuration")
		os.Exit(1)
	}
	var cfg clusteragent.Config
	if err = json.Unmarshal(data, &cfg); err != nil {
		slog.Error("invalid agent configuration")
		os.Exit(1)
	}
	kube, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("agent must run inside its target Kubernetes cluster")
		os.Exit(1)
	}
	agent, err := clusteragent.New(cfg, kube)
	if err != nil {
		slog.Error("agent configuration rejected", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if err = agent.Run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
