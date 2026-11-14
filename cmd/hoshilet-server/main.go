package main

import (
	"context"
	"flag"
	"fmt"
	"hoshilet/internal/archive"
	"hoshilet/internal/platform"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if len(os.Args) > 1 && os.Args[1] == "extract" {
		if e := archive.Worker(os.Args[2:]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	config := flag.String("config", "config.json", "Configuration JSON path")
	backup := flag.String("backup", "", "Write a backup tar.gz while server is stopped")
	restore := flag.String("restore", "", "Restore a trusted backup into a non-existent data directory")
	check := flag.Bool("check", false, "Validate configuration and exit")
	showVersion := flag.Bool("version", false, "Print version and build information and exit")
	flag.Parse()
	if *showVersion {
		fmt.Print(versionInfo())
		return
	}
	c, e := platform.LoadConfig(*config)
	if e != nil {
		slog.Error("configuration", "error", e)
		os.Exit(1)
	}
	if *check {
		return
	}
	if *restore != "" {
		if *backup != "" {
			slog.Error("choose backup or restore")
			os.Exit(1)
		}
		if e = platform.Restore(c, *restore); e != nil {
			slog.Error("restore", "error", e)
			os.Exit(1)
		}
		return
	}
	a, e := platform.Open(c)
	if e != nil {
		slog.Error("startup", "error", e)
		os.Exit(1)
	}
	defer a.Close()
	if *backup != "" {
		if e = a.Backup(*backup); e != nil {
			slog.Error("backup", "error", e)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go a.Maintenance(ctx.Done())
	srv := &http.Server{Addr: c.Listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: time.Duration(c.UploadTimeoutSeconds+30) * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("management server listening", "address", c.Listen)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("server", "error", e)
		os.Exit(1)
	}
}
