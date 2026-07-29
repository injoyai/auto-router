package jwt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIssueAndVerify(t *testing.T) {
	mgr := New("my-secret")
	tok, err := mgr.Issue("admin")
	assert.NoError(t, err)
	assert.NotEmpty(t, tok)
	claims, err := mgr.Verify(tok)
	assert.NoError(t, err)
	assert.Equal(t, "admin", claims.Subject)
}

func TestVerifyInvalid(t *testing.T) {
	mgr := New("my-secret")
	_, err := mgr.Verify("not-a-token")
	assert.Error(t, err)
}
