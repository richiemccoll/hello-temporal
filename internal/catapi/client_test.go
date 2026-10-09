package catapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/hello-temporal/internal/catapi"
)

func TestRandomCat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		body          string
		wantCat       catapi.Cat
		wantErr       error
		wantStatus    int
		wantRetryable bool
	}{
		{
			name:    "success",
			status:  http.StatusOK,
			body:    `[{"id":"abc","url":"https://cdn/abc.jpg","width":640,"height":480}]`,
			wantCat: catapi.Cat{ID: "abc", URL: "https://cdn/abc.jpg", Width: 640, Height: 480},
		},
		{name: "empty list", status: http.StatusOK, body: `[]`, wantErr: catapi.ErrNoCats},
		{name: "server error is retryable", status: http.StatusBadGateway, body: "upstream down", wantStatus: 502, wantRetryable: true},
		{name: "rate limit is retryable", status: http.StatusTooManyRequests, wantStatus: 429, wantRetryable: true},
		{name: "unauthorized is not retryable", status: http.StatusUnauthorized, body: "bad key", wantStatus: 401},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/images/search", r.URL.Path)
				assert.Equal(t, "1", r.URL.Query().Get("limit"))
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			cat, err := catapi.New(srv.URL).RandomCat(t.Context())

			switch {
			case tc.wantStatus != 0:
				var se *catapi.StatusError
				require.ErrorAs(t, err, &se)
				assert.Equal(t, tc.wantStatus, se.StatusCode)
				assert.Equal(t, tc.wantRetryable, se.Retryable())
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			default:
				require.NoError(t, err)
				assert.Equal(t, tc.wantCat, cat)
			}
		})
	}
}

func TestRandomCatSendsAPIKey(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secret", r.Header.Get("x-api-key"))
		_, _ = w.Write([]byte(`[{"id":"k"}]`))
	}))
	t.Cleanup(srv.Close)

	cat, err := catapi.New(srv.URL+"/", catapi.WithAPIKey("secret")).RandomCat(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "k", cat.ID)
}

func TestRandomCatInvalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	t.Cleanup(srv.Close)

	_, err := catapi.New(srv.URL).RandomCat(t.Context())
	require.ErrorContains(t, err, "decode response")
}

func TestRandomCatHonoursContext(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := catapi.New(srv.URL).RandomCat(ctx)
	require.True(t, errors.Is(err, context.Canceled), "got %v", err)
}
