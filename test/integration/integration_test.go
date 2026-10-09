//go:build integration

// Package integration exercises the API, a real Temporal server, a real worker and an
// HTTP fake of TheCatAPI together, in-process. Envoy is covered by the e2e suite.
//
// Run with: go test -tags integration ./test/integration/...
package integration

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	hellov1 "github.com/example/hello-temporal/gen/hello/v1"
	"github.com/example/hello-temporal/gen/hello/v1/hellov1connect"
	"github.com/example/hello-temporal/internal/apiserver"
	"github.com/example/hello-temporal/internal/catapi"
	"github.com/example/hello-temporal/internal/hello"
)

var temporalClient client.Client

func TestMain(m *testing.M) {
	opts := testsuite.DevServerOptions{LogLevel: "error"}
	// Prefer a locally installed Temporal CLI; otherwise the SDK downloads and caches one.
	if path, err := exec.LookPath("temporal"); err == nil {
		opts.ExistingPath = path
	}
	srv, err := testsuite.StartDevServer(context.Background(), opts)
	if err != nil {
		log.Fatalf("start temporal dev server: %v", err)
	}
	temporalClient = srv.Client()

	code := m.Run()

	temporalClient.Close()
	_ = srv.Stop()
	os.Exit(code)
}

// fakeCatAPI mimics TheCatAPI. Responses are served in order; the last one repeats.
type fakeCatAPI struct {
	responses []fakeResponse
	calls     atomic.Int32
	apiKeys   chan string
}

type fakeResponse struct {
	status int
	body   string
}

func newFakeCatAPI(t *testing.T, responses ...fakeResponse) (*fakeCatAPI, string) {
	t.Helper()
	f := &fakeCatAPI{responses: responses, apiKeys: make(chan string, 16)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(f.calls.Add(1)) - 1
		f.apiKeys <- r.Header.Get("x-api-key")
		resp := f.responses[min(n, len(f.responses)-1)]
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

// startStack runs a worker and the API on an isolated task queue and returns a gRPC client and the queue name.
func startStack(t *testing.T, catAPIURL string) (hellov1connect.HelloServiceClient, string) {
	t.Helper()
	taskQueue := "it-" + uuid.NewString()

	w := worker.New(temporalClient, taskQueue, worker.Options{})
	hello.Register(w, &hello.Activities{Cats: catapi.New(catAPIURL, catapi.WithAPIKey("test-key"))})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)

	svc := apiserver.New(temporalClient, apiserver.WithTaskQueue(taskQueue), apiserver.WithWorkflowTimeout(time.Minute))
	api := httptest.NewUnstartedServer(apiserver.NewHTTPHandler(svc))
	api.EnableHTTP2 = true
	api.StartTLS()
	t.Cleanup(api.Close)

	// Use the gRPC protocol (not Connect) to prove the server is a real gRPC endpoint.
	return hellov1connect.NewHelloServiceClient(api.Client(), api.URL, connect.WithGRPC()), taskQueue
}

func describe(t *testing.T, workflowID string) enumspb.WorkflowExecutionStatus {
	t.Helper()
	desc, err := temporalClient.DescribeWorkflowExecution(t.Context(), workflowID, "")
	require.NoError(t, err)
	return desc.GetWorkflowExecutionInfo().GetStatus()
}

func TestSayHello_HappyPath(t *testing.T) {
	t.Parallel()
	fake, url := newFakeCatAPI(t, fakeResponse{http.StatusOK, `[{"id":"it1","url":"https://cdn/it1.jpg","width":800,"height":600}]`})
	c, _ := startStack(t, url)

	resp, err := c.SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: "Grace"}))
	require.NoError(t, err)

	assert.Equal(t, "Hello, Grace! Here is a cat for you.", resp.Msg.GetGreeting())
	assert.Equal(t, "it1", resp.Msg.GetCat().GetId())
	assert.Equal(t, "https://cdn/it1.jpg", resp.Msg.GetCat().GetUrl())
	assert.NotEmpty(t, resp.Msg.GetRunId())
	assert.Equal(t, int32(1), fake.calls.Load())
	assert.Equal(t, "test-key", <-fake.apiKeys)
	assert.Equal(t, enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED, describe(t, resp.Msg.GetWorkflowId()))
}

func TestSayHello_RetriesTransientCatAPIFailure(t *testing.T) {
	t.Parallel()
	fake, url := newFakeCatAPI(t,
		fakeResponse{http.StatusServiceUnavailable, "try later"},
		fakeResponse{http.StatusOK, `[{"id":"it2","url":"https://cdn/it2.jpg"}]`},
	)
	c, _ := startStack(t, url)

	resp, err := c.SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: "Linus"}))
	require.NoError(t, err)

	assert.Equal(t, "it2", resp.Msg.GetCat().GetId())
	assert.Equal(t, int32(2), fake.calls.Load(), "Temporal should have retried the activity once")
}

func TestSayHello_NonRetryableCatAPIFailure(t *testing.T) {
	t.Parallel()
	fake, url := newFakeCatAPI(t, fakeResponse{http.StatusUnauthorized, "invalid api key"})
	c, taskQueue := startStack(t, url)

	_, err := c.SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: "Barbara"}))
	require.Error(t, err)

	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	assert.Equal(t, int32(1), fake.calls.Load(), "4xx must not be retried")

	// The workflow ID is not returned on failure, so find it via visibility.
	require.Eventually(t, func() bool {
		resp, err := temporalClient.ListWorkflow(t.Context(), &workflowservice.ListWorkflowExecutionsRequest{
			Query: fmt.Sprintf("ExecutionStatus = 'Failed' AND TaskQueue = '%s'", taskQueue),
		})
		return err == nil && len(resp.GetExecutions()) == 1
	}, 10*time.Second, 200*time.Millisecond)
}

func TestSayHello_InvalidInputRejectedBeforeTemporal(t *testing.T) {
	t.Parallel()
	fake, url := newFakeCatAPI(t, fakeResponse{http.StatusOK, `[]`})
	c, _ := startStack(t, url)

	_, err := c.SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: ""}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Equal(t, int32(0), fake.calls.Load())
}
