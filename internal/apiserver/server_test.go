package apiserver_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"

	hellov1 "github.com/example/hello-temporal/gen/hello/v1"
	"github.com/example/hello-temporal/gen/hello/v1/hellov1connect"
	"github.com/example/hello-temporal/internal/apiserver"
	"github.com/example/hello-temporal/internal/hello"
)

// newClient serves the real HTTP handler (incl. protovalidate) and returns a Connect client for it.
func newClient(t *testing.T, tc *mocks.Client) hellov1connect.HelloServiceClient {
	t.Helper()
	svc := apiserver.New(tc, apiserver.WithIDGenerator(func() string { return "fixed" }))
	srv := httptest.NewServer(apiserver.NewHTTPHandler(svc))
	t.Cleanup(srv.Close)
	return hellov1connect.NewHelloServiceClient(srv.Client(), srv.URL)
}

func TestSayHelloSuccess(t *testing.T) {
	t.Parallel()
	tc := mocks.NewClient(t)
	run := mocks.NewWorkflowRun(t)

	tc.On("ExecuteWorkflow", mock.Anything,
		mock.MatchedBy(func(o client.StartWorkflowOptions) bool {
			return o.ID == "say-hello-fixed" && o.TaskQueue == hello.TaskQueue && o.WorkflowExecutionTimeout > 0
		}),
		mock.Anything, hello.SayHelloInput{Name: "Ada"},
	).Return(run, nil)
	run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		*args.Get(1).(*hello.SayHelloOutput) = hello.SayHelloOutput{
			Greeting: "Hello, Ada!",
			Cat:      hello.Cat{ID: "c1", URL: "https://cdn/c1.png", Width: 3, Height: 4},
		}
	}).Return(nil)
	run.On("GetID").Return("say-hello-fixed")
	run.On("GetRunID").Return("run-1")

	resp, err := newClient(t, tc).SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: "Ada"}))
	require.NoError(t, err)

	assert.Equal(t, "Hello, Ada!", resp.Msg.GetGreeting())
	assert.Equal(t, "say-hello-fixed", resp.Msg.GetWorkflowId())
	assert.Equal(t, "run-1", resp.Msg.GetRunId())
	assert.Equal(t, "c1", resp.Msg.GetCat().GetId())
	assert.Equal(t, int32(4), resp.Msg.GetCat().GetHeight())
}

func TestSayHelloValidation(t *testing.T) {
	t.Parallel()
	tc := mocks.NewClient(t) // no expectations: Temporal must not be called

	for _, name := range []string{"", string(make([]byte, 65))} {
		_, err := newClient(t, tc).SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: name}))
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name len %d", len(name))
	}
}

func TestSayHelloErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		startErr error
		runErr   error
		want     connect.Code
	}{
		{name: "temporal unavailable", startErr: errors.New("dial failed"), want: connect.CodeUnavailable},
		{name: "workflow failed", runErr: errors.New("activity failed"), want: connect.CodeInternal},
		{name: "workflow timed out", runErr: temporal.NewTimeoutError(0, nil), want: connect.CodeDeadlineExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			temporalClient := mocks.NewClient(t)
			if tc.startErr != nil {
				temporalClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, tc.startErr)
			} else {
				run := mocks.NewWorkflowRun(t)
				run.On("Get", mock.Anything, mock.Anything).Return(tc.runErr)
				temporalClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(run, nil)
			}

			_, err := newClient(t, temporalClient).SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: "Ada"}))
			assert.Equal(t, tc.want, connect.CodeOf(err))
		})
	}
}

func TestHealthEndpointServed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(apiserver.NewHTTPHandler(apiserver.New(mocks.NewClient(t))))
	t.Cleanup(srv.Close)

	// Connect protocol unary call to grpc.health.v1.Health/Check with an empty JSON message.
	resp, err := srv.Client().Post(srv.URL+"/grpc.health.v1.Health/Check", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
