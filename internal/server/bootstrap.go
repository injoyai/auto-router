package server

import (
	"crypto/rand"
	"encoding/hex"

	"auto-router/internal/store"
)

const (
	settingCryptoSeed   = "crypto_seed"
	settingGatewayToken = "gateway_token"
	settingAdminToken   = "admin_token"
)

// Bootstrap loads (or generates + persists) the crypto seed, gateway token,
// and admin token from the store settings. On the first run all three are
// generated with cryptographic random bytes and persisted; subsequent calls
// return the persisted values unchanged.
func Bootstrap(st *store.Store) (key []byte, gatewayToken, adminToken string, err error) {
	seed, err := getOrCreateSetting(st, settingCryptoSeed, randomHex(32))
	if err != nil {
		return nil, "", "", err
	}
	key = store.DeriveKey(seed)
	gatewayToken, err = getOrCreateSetting(st, settingGatewayToken, randomHex(24))
	if err != nil {
		return nil, "", "", err
	}
	adminToken, err = getOrCreateSetting(st, settingAdminToken, randomHex(24))
	if err != nil {
		return nil, "", "", err
	}
	return key, gatewayToken, adminToken, nil
}

// getOrCreateSetting returns the persisted value for key, or persists gen and
// returns it when no value is present yet.
func getOrCreateSetting(st *store.Store, key, gen string) (string, error) {
	if v, err := st.GetSetting(key); err == nil && v != "" {
		return v, nil
	}
	if err := st.SetSetting(key, gen); err != nil {
		return "", err
	}
	return gen, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
