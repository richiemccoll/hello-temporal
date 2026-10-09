package envconfig_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/hello-temporal/internal/envconfig"
)

func TestString(t *testing.T) {
	t.Setenv("ENVCONFIG_TEST", "")
	assert.Equal(t, "def", envconfig.String("ENVCONFIG_TEST", "def"))
	t.Setenv("ENVCONFIG_TEST", "set")
	assert.Equal(t, "set", envconfig.String("ENVCONFIG_TEST", "def"))
}

func TestDuration(t *testing.T) {
	t.Setenv("ENVCONFIG_DUR", "")
	d, err := envconfig.Duration("ENVCONFIG_DUR", time.Second)
	require.NoError(t, err)
	assert.Equal(t, time.Second, d)

	t.Setenv("ENVCONFIG_DUR", "250ms")
	d, err = envconfig.Duration("ENVCONFIG_DUR", time.Second)
	require.NoError(t, err)
	assert.Equal(t, 250*time.Millisecond, d)

	t.Setenv("ENVCONFIG_DUR", "soon")
	_, err = envconfig.Duration("ENVCONFIG_DUR", time.Second)
	require.Error(t, err)
}
