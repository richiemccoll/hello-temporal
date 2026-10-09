// Command worker polls the Temporal task queue and executes SayHello workflows and activities.
package main

import (
	"log/slog"
	"os"

	"go.temporal.io/sdk/client"
	tlog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"

	"github.com/example/hello-temporal/internal/catapi"
	"github.com/example/hello-temporal/internal/envconfig"
	"github.com/example/hello-temporal/internal/hello"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "worker")
	if err := run(logger); err != nil {
		logger.Error("worker exited", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	tc, err := client.Dial(client.Options{
		HostPort:  envconfig.String("TEMPORAL_ADDRESS", client.DefaultHostPort),
		Namespace: envconfig.String("TEMPORAL_NAMESPACE", client.DefaultNamespace),
		Logger:    tlog.NewStructuredLogger(logger),
	})
	if err != nil {
		return err
	}
	defer tc.Close()

	// By default the activity calls Envoy's egress listener, not the internet directly.
	cats := catapi.New(
		envconfig.String("CAT_API_BASE_URL", "http://127.0.0.1:10000"),
		catapi.WithAPIKey(os.Getenv("CAT_API_KEY")),
	)

	w := worker.New(tc, hello.TaskQueue, worker.Options{})
	hello.Register(w, &hello.Activities{Cats: cats})

	logger.Info("worker started", "taskQueue", hello.TaskQueue)
	return w.Run(worker.InterruptCh())
}
