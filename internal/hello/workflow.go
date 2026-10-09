// Package hello contains the Temporal workflow and activities for the SayHello use case.
//
// Workflow code must be deterministic: it only orchestrates. Anything that talks to
// the outside world (here: TheCatAPI) lives in an activity, which Temporal records,
// times out and retries independently.
package hello

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// TaskQueue is the Temporal task queue shared by the API (starter) and the worker.
const TaskQueue = "hello-cats"

// SayHelloInput is the workflow input. Use structs (not positional args) so fields can be added compatibly.
type SayHelloInput struct {
	Name string
}

// SayHelloOutput is the workflow result.
type SayHelloOutput struct {
	Greeting string
	Cat      Cat
}

// Cat is the workflow's own representation of a cat, decoupled from the HTTP client's type.
type Cat struct {
	ID     string
	URL    string
	Width  int
	Height int
}

// ActivityOptions used for FetchCat. Exported so tests and docs can refer to them.
var ActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 10 * time.Second,
	RetryPolicy: &temporal.RetryPolicy{
		InitialInterval:        time.Second,
		BackoffCoefficient:     2.0,
		MaximumInterval:        10 * time.Second,
		MaximumAttempts:        5,
		NonRetryableErrorTypes: []string{ErrTypeCatAPIClient},
	},
}

// SayHello greets the caller and fetches a random cat via the FetchCat activity.
func SayHello(ctx workflow.Context, in SayHelloInput) (SayHelloOutput, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("SayHello workflow started", "name", in.Name)

	ctx = workflow.WithActivityOptions(ctx, ActivityOptions)

	var a *Activities // nil receiver is fine: only the method reference is used to name the activity.
	var cat Cat
	if err := workflow.ExecuteActivity(ctx, a.FetchCat).Get(ctx, &cat); err != nil {
		return SayHelloOutput{}, fmt.Errorf("fetch cat: %w", err)
	}

	out := SayHelloOutput{
		Greeting: fmt.Sprintf("Hello, %s! Here is a cat for you.", in.Name),
		Cat:      cat,
	}
	logger.Info("SayHello workflow completed", "catID", cat.ID)
	return out, nil
}

// Register registers the workflow and activities with a worker.
func Register(r worker.Registry, activities *Activities) {
	r.RegisterWorkflow(SayHello)
	r.RegisterActivity(activities)
}
