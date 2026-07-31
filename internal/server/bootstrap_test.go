package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"auto-router/internal/store"
)

func TestBootstrapGeneratesMissingSecrets(t *testing.T) {
	st, err := store.Open(store.SQLiteDialer{}, ":memory:")
	assert.NoError(t, err)
	key, gw, admin, err := Bootstrap(st)
	assert.NoError(t, err)
	assert.Len(t, key, 32)
	assert.NotEmpty(t, gw)
	assert.NotEmpty(t, admin)

	// second call returns same values (persisted)
	key2, gw2, admin2, err := Bootstrap(st)
	assert.NoError(t, err)
	assert.Equal(t, key, key2)
	assert.Equal(t, gw, gw2)
	assert.Equal(t, admin, admin2)
}
