package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := DeriveKey("test-seed")
	enc := Encrypt(key, "sk-secret-123")
	assert.NotEqual(t, "sk-secret-123", enc)
	dec, err := Decrypt(key, enc)
	assert.NoError(t, err)
	assert.Equal(t, "sk-secret-123", dec)
}

func TestDeriveKeyDeterministic(t *testing.T) {
	assert.Equal(t, DeriveKey("seed-a"), DeriveKey("seed-a"))
	assert.NotEqual(t, DeriveKey("seed-a"), DeriveKey("seed-b"))
}
