package hello_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/example/hello-temporal/internal/catapi"
	"github.com/example/hello-temporal/internal/hello"
)

type fakeCats struct {
	cat catapi.Cat
	err error
}

func (f fakeCats) RandomCat(context.Context) (catapi.Cat, error) { return f.cat, f.err }

func newActivityEnv(t *testing.T, cats hello.CatFetcher) *testsuite.TestActivityEnvironment {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestActivityEnvironment()
	env.RegisterActivity(&hello.Activities{Cats: cats})
	return env
}

func TestFetchCat(t *testing.T) {
	t.Parallel()
	env := newActivityEnv(t, fakeCats{cat: catapi.Cat{ID: "c1", URL: "https://cdn/c1.png", Width: 10, Height: 20}})

	val, err := env.ExecuteActivity((&hello.Activities{}).FetchCat)
	require.NoError(t, err)

	var got hello.Cat
	require.NoError(t, val.Get(&got))
	assert.Equal(t, hello.Cat{ID: "c1", URL: "https://cdn/c1.png", Width: 10, Height: 20}, got)
}

func TestFetchCatClientErrorIsNonRetryable(t *testing.T) {
	t.Parallel()
	env := newActivityEnv(t, fakeCats{err: &catapi.StatusError{StatusCode: 401, Body: "nope"}})

	_, err := env.ExecuteActivity((&hello.Activities{}).FetchCat)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable())
	assert.Equal(t, hello.ErrTypeCatAPIClient, appErr.Type())
}

func TestFetchCatTransientErrorIsRetryable(t *testing.T) {
	t.Parallel()
	env := newActivityEnv(t, fakeCats{err: &catapi.StatusError{StatusCode: 503}})

	_, err := env.ExecuteActivity((&hello.Activities{}).FetchCat)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.False(t, appErr.NonRetryable())

	_, err = newActivityEnv(t, fakeCats{err: errors.New("connection reset")}).ExecuteActivity((&hello.Activities{}).FetchCat)
	require.ErrorAs(t, err, &appErr)
	assert.False(t, appErr.NonRetryable())
}
