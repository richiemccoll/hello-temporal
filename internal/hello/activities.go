package hello

import (
	"context"
	"errors"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/example/hello-temporal/internal/catapi"
)

// ErrTypeCatAPIClient marks failures that retrying will not fix (e.g. 401/404 from TheCatAPI).
const ErrTypeCatAPIClient = "CatAPIClientError"

// CatFetcher is the dependency the activity needs. *catapi.Client satisfies it.
type CatFetcher interface {
	RandomCat(ctx context.Context) (catapi.Cat, error)
}

// Activities holds activity dependencies. Register a pointer to it with the worker.
type Activities struct {
	Cats CatFetcher
}

// FetchCat calls TheCatAPI and returns one random cat.
func (a *Activities) FetchCat(ctx context.Context) (Cat, error) {
	info := activity.GetInfo(ctx)
	activity.GetLogger(ctx).Info("fetching cat", "attempt", info.Attempt)

	c, err := a.Cats.RandomCat(ctx)
	if err != nil {
		var statusErr *catapi.StatusError
		if errors.As(err, &statusErr) && !statusErr.Retryable() {
			return Cat{}, temporal.NewNonRetryableApplicationError(err.Error(), ErrTypeCatAPIClient, err)
		}
		return Cat{}, err // transient: let Temporal retry per the activity RetryPolicy
	}
	return Cat{ID: c.ID, URL: c.URL, Width: c.Width, Height: c.Height}, nil
}
