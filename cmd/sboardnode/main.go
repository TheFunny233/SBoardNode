package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/TheFunny233/SBoardNode/internal/agent"
	"github.com/TheFunny233/SBoardNode/internal/config"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to config.json")
	once := flag.Bool("once", false, "send one heartbeat and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sboardnode %s (commit=%s, built=%s)\n", version, commit, date)
		return
	}

	logger := log.New(os.Stderr, "sboardnode: ", log.Ldate|log.Ltime|log.LUTC)
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	runner, err := agent.New(cfg, version, logger)
	if err != nil {
		logger.Fatalf("initialize: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *once {
		if err := runner.RunOnce(ctx); err != nil {
			logger.Fatalf("heartbeat: %v", err)
		}
		return
	}

	logger.Printf("starting version=%s node_id=%s interval=%s", version, cfg.NodeID, cfg.Interval())
	if err := runner.Run(ctx); err != nil {
		logger.Fatalf("run: %v", err)
	}
}
