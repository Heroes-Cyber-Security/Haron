package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	forwarder "hanz.dev/blockchain/forwarder/service"
)

func main() {
	logger := log.New(os.Stdout, "forwarder ", log.LstdFlags|log.Lmsgprefix)

	orchestratorURL := getEnv("ORCHESTRATOR_URL", "http://orchestrator:8080")
	listenAddr := getEnv("FORWARDER_LISTEN_ADDR", ":8080")

	service, err := forwarder.NewService(forwarder.Config{
		OrchestratorURL: orchestratorURL,
		Logger:          logger,
	})
	if err != nil {
		logger.Fatalf("failed to create forwarder: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/eth/", service.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  90 * time.Second,
	}

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Printf("listening on %s, forwarding to %s", listenAddr, orchestratorURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server error: %v", err)
		}
	}()

	<-shutdownCh
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Println("shutting down")
	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
