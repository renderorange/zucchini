package main

import (
    "context"
    "flag"
    "log"
    "os"
    "os/signal"
    "syscall"

    "zucchini/internal/bluez"
    "zucchini/internal/config"
    "zucchini/internal/loop"
    "zucchini/internal/matcher"
)

func main() {
    configPath := flag.String("config", "/etc/zucchini.json", "path to config file")
    flag.Parse()

    logger := log.New(os.Stdout, "zucchini ", log.LstdFlags|log.Lmsgprefix)

    cfg, err := config.Load(*configPath)
    if err != nil {
        logger.Fatalf("config: %v", err)
    }

    m, err := matcher.New(cfg.Signatures)
    if err != nil {
        logger.Fatalf("matcher: %v", err)
    }

    client, err := bluez.Dial()
    if err != nil {
        logger.Fatalf("bluez: %v", err)
    }
    defer client.Close()

    adapter, err := client.AdapterPath(cfg.Adapter)
    if err != nil {
        logger.Fatalf("adapter: %v", err)
    }

    logger.Printf("starting: adapter=%s grace=%s workers=%d call_timeout=%s attempt_gap=%s",
        adapter, cfg.Grace(), cfg.Workers, cfg.CallTimeout(), cfg.AttemptGap())

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    runner := loop.New(cfg, m, client, logger)
    if err := runner.Run(ctx, adapter); err != nil && err != context.Canceled {
        logger.Fatalf("run: %v", err)
    }
    logger.Printf("stopped")
}
