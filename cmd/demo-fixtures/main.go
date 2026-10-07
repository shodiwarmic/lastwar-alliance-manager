// demo-fixtures is the demo's stand-in OCR service and Collabora (internal/fixtures):
//
//	go run ./cmd/demo-fixtures -port 9090 -frame-ancestors http://localhost:8080
//
// Configured by flags rather than environment variables; on Cloud Run they are the
// container's args.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lastwar-alliance/internal/fixtures"
)

func main() {
	port := flag.String("port", "8080", "port to listen on")
	frameAncestors := flag.String("frame-ancestors", "", "origin allowed to embed the document page (the demo app), e.g. https://demo.example")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	srv := &http.Server{
		Addr:              ":" + *port,
		Handler:           fixtures.Handler(fixtures.Config{FrameAncestors: *frameAncestors}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("Demo fixtures listening", "port", *port, "version", fixtures.Version, "frame_ancestors", *frameAncestors)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
