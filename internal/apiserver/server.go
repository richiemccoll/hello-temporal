// Package apiserver implements the hello.v1.HelloService Connect handler.
//
// The handler is a thin adapter: it validates input (protovalidate interceptor),
// starts a Temporal workflow and maps the result back to protobuf.
package apiserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/grpcreflect"
	"connectrpc.com/validate"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	hellov1 "github.com/example/hello-temporal/gen/hello/v1"
	"github.com/example/hello-temporal/gen/hello/v1/hellov1connect"
	"github.com/example/hello-temporal/internal/hello"
)

// WorkflowStarter is the subset of client.Client the server needs; it keeps tests small.
type WorkflowStarter interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
}

// Server implements hellov1connect.HelloServiceHandler.
type Server struct {
	temporal        WorkflowStarter
	taskQueue       string
	workflowTimeout time.Duration
	newID           func() string
}

var _ hellov1connect.HelloServiceHandler = (*Server)(nil)

// Option configures a Server.
type Option func(*Server)

// WithTaskQueue overrides the Temporal task queue (default hello.TaskQueue).
func WithTaskQueue(q string) Option { return func(s *Server) { s.taskQueue = q } }

// WithWorkflowTimeout bounds the total workflow execution time (default 30s).
func WithWorkflowTimeout(d time.Duration) Option { return func(s *Server) { s.workflowTimeout = d } }

// WithIDGenerator overrides how workflow ID suffixes are generated (useful in tests).
func WithIDGenerator(f func() string) Option { return func(s *Server) { s.newID = f } }

// New returns a Server backed by the given Temporal client.
func New(t WorkflowStarter, opts ...Option) *Server {
	s := &Server{
		temporal:        t,
		taskQueue:       hello.TaskQueue,
		workflowTimeout: 30 * time.Second,
		newID:           uuid.NewString,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SayHello starts the SayHello workflow and waits for its result.
func (s *Server) SayHello(ctx context.Context, req *connect.Request[hellov1.SayHelloRequest]) (*connect.Response[hellov1.SayHelloResponse], error) {
	opts := client.StartWorkflowOptions{
		ID:                       "say-hello-" + s.newID(),
		TaskQueue:                s.taskQueue,
		WorkflowExecutionTimeout: s.workflowTimeout,
	}
	run, err := s.temporal.ExecuteWorkflow(ctx, opts, hello.SayHello, hello.SayHelloInput{Name: req.Msg.GetName()})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("start workflow: %w", err))
	}

	var out hello.SayHelloOutput
	if err := run.Get(ctx, &out); err != nil {
		return nil, toConnectError(ctx, err)
	}

	return connect.NewResponse(&hellov1.SayHelloResponse{
		Greeting:   out.Greeting,
		WorkflowId: run.GetID(),
		RunId:      run.GetRunID(),
		Cat: &hellov1.Cat{
			Id:     out.Cat.ID,
			Url:    out.Cat.URL,
			Width:  int32(out.Cat.Width),
			Height: int32(out.Cat.Height),
		},
	}), nil
}

func toConnectError(ctx context.Context, err error) error {
	var timeoutErr *temporal.TimeoutError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.As(err, &timeoutErr):
		return connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf("workflow timed out: %w", err))
	case errors.Is(ctx.Err(), context.Canceled):
		return connect.NewError(connect.CodeCanceled, err)
	default:
		return connect.NewError(connect.CodeInternal, fmt.Errorf("workflow failed: %w", err))
	}
}

// NewHTTPHandler wires the service, protovalidate, gRPC health checks and gRPC reflection into one mux.
func NewHTTPHandler(svc hellov1connect.HelloServiceHandler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(hellov1connect.NewHelloServiceHandler(svc, connect.WithInterceptors(validate.NewInterceptor())))

	checker := grpchealth.NewStaticChecker(hellov1connect.HelloServiceName)
	mux.Handle(grpchealth.NewHandler(checker))

	reflector := grpcreflect.NewStaticReflector(hellov1connect.HelloServiceName)
	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
	return mux
}
