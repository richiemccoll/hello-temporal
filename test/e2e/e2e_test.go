//go:build e2e

// Package e2e calls the running stack through Envoy, which calls the real TheCatAPI.
// Start the stack first with `make up`, then run `make test-e2e`.
package e2e

import (
	"bufio"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hellov1 "github.com/example/hello-temporal/gen/hello/v1"
	"github.com/example/hello-temporal/gen/hello/v1/hellov1connect"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var (
	baseURL  = env("E2E_BASE_URL", "http://127.0.0.1:9080")
	adminURL = env("E2E_ENVOY_ADMIN_URL", "http://127.0.0.1:9901")
)

// h2cClient speaks HTTP/2 over cleartext, which gRPC through Envoy's plaintext listener needs.
func h2cClient() *http.Client {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Client{
		Timeout:   40 * time.Second,
		Transport: &http.Transport{Protocols: protocols},
	}
}

// envoyCounter reads a single counter from the Envoy admin /stats endpoint.
func envoyCounter(t *testing.T, name string) int {
	t.Helper()
	resp, err := http.Get(adminURL + "/stats?filter=^" + name + "$")
	require.NoError(t, err)
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), ": "); ok && k == name {
			n, err := strconv.Atoi(v)
			require.NoError(t, err)
			return n
		}
	}
	t.Fatalf("counter %s not found", name)
	return 0
}

func assertCat(t *testing.T, resp *connect.Response[hellov1.SayHelloResponse], name string) {
	t.Helper()
	assert.Equal(t, "Hello, "+name+"! Here is a cat for you.", resp.Msg.GetGreeting())
	assert.NotEmpty(t, resp.Msg.GetCat().GetId())
	assert.True(t, strings.HasPrefix(resp.Msg.GetCat().GetUrl(), "https://"), "cat url: %q", resp.Msg.GetCat().GetUrl())
	assert.True(t, strings.HasPrefix(resp.Msg.GetWorkflowId(), "say-hello-"))
	assert.NotEmpty(t, resp.Msg.GetRunId())
}

func TestSayHello_ConnectProtocolViaEnvoy(t *testing.T) {
	ingressBefore := envoyCounter(t, "cluster.api.upstream_rq_total")
	egressBefore := envoyCounter(t, "cluster.thecatapi.upstream_rq_total")

	c := hellov1connect.NewHelloServiceClient(&http.Client{Timeout: 40 * time.Second}, baseURL, connect.WithProtoJSON())
	resp, err := c.SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: "E2E"}))
	require.NoError(t, err)
	assertCat(t, resp, "E2E")

	// Prove both hops actually went through Envoy.
	assert.Greater(t, envoyCounter(t, "cluster.api.upstream_rq_total"), ingressBefore, "ingress hop")
	assert.Greater(t, envoyCounter(t, "cluster.thecatapi.upstream_rq_total"), egressBefore, "egress hop")
}

func TestSayHello_GRPCProtocolViaEnvoy(t *testing.T) {
	c := hellov1connect.NewHelloServiceClient(h2cClient(), baseURL, connect.WithGRPC())
	resp, err := c.SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{Name: "gRPC"}))
	require.NoError(t, err)
	assertCat(t, resp, "gRPC")
}

func TestSayHello_ValidationViaEnvoy(t *testing.T) {
	c := hellov1connect.NewHelloServiceClient(h2cClient(), baseURL, connect.WithGRPC())
	_, err := c.SayHello(t.Context(), connect.NewRequest(&hellov1.SayHelloRequest{}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
