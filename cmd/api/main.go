// Command api serves hello.v1.HelloService (Connect, gRPC and gRPC-Web) behind Envoy.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	tlog "go.temporal.io/sdk/log"

	"github.com/example/hello-temporal/internal/apiserver"
	"github.com/example/hello-temporal/internal/envconfig"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "api")
	if err := run(logger); err != nil {
		logger.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := envconfig.String("API_ADDR", "127.0.0.1:8081")
	wfTimeout, err := envconfig.Duration("WORKFLOW_TIMEOUT", 30*time.Second)
	if err != nil {
		return err
	}

	tc, err := client.Dial(client.Options{
		HostPort:  envconfig.String("TEMPORAL_ADDRESS", client.DefaultHostPort),
		Namespace: envconfig.String("TEMPORAL_NAMESPACE", client.DefaultNamespace),
		Logger:    tlog.NewStructuredLogger(logger),
	})
	if err != nil {
		return err
	}
	defer tc.Close()

	// Envoy speaks HTTP/2 cleartext (h2c) to this upstream so gRPC works end to end;
	// HTTP/1.1 stays enabled for Connect/JSON callers.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{
		Addr:              addr,
		Handler:           apiserver.NewHTTPHandler(apiserver.New(tc, apiserver.WithWorkflowTimeout(wfTimeout))),
		Protocols:         protocols,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
