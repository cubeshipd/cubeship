package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"cubeship/internal/api"
	"cubeship/internal/bootstrap"
	"cubeship/internal/config"
	"cubeship/internal/deploy"
	"cubeship/internal/dockerx"
	"cubeship/internal/reconcile"
	"cubeship/internal/store"
)

const version = "0.1.0-dev"
const daemonPort = 9000
const listenAddr = ":9000"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cubeshipd %s\n", version)
		os.Exit(0)
	}

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log.Printf("cubeshipd starting for domain %s", cfg.Domain)
	log.Printf("daemon API token: %s", cfg.Token)

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	docker, err := dockerx.New()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}

	ctx := context.Background()

	if err := docker.EnsureNetwork(ctx, "cubeship"); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	s, err := store.Open(cfg.DataDir + "/cubeship.db")
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	notifyURL := "http://127.0.0.1" + listenAddr + "/hooks/registry"
	if err := bootstrap.Ensure(ctx, docker, bootstrap.RegistryContainerOpts(cfg, notifyURL)); err != nil {
		return fmt.Errorf("bootstrap registry: %w", err)
	}
	if err := bootstrap.WriteAPIRouterConfig(cfg, daemonPort); err != nil {
		return fmt.Errorf("write traefik API router config: %w", err)
	}
	if err := bootstrap.Ensure(ctx, docker, bootstrap.TraefikContainerOpts(cfg, cfg.AcmeEmail)); err != nil {
		return fmt.Errorf("bootstrap traefik: %w", err)
	}

	if err := reconcile.Run(ctx, s, docker); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	orch := deploy.NewOrchestrator(s, docker)
	srv := api.NewServer(s, orch, cfg.Token, cfg.RegistryHost)

	log.Printf("cubeshipd listening on %s", listenAddr)
	return http.ListenAndServe(listenAddr, srv.Router())
}
