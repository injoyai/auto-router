package server

import (
	"testing"

	"auto-router/internal/config"
	"auto-router/internal/store"
)

type testApp struct {
	*App
	UpstreamURL string
}

// newTestApp seeds an in-memory store with one provider (pointed at upstreamURL
// with encrypted key "sk-test"), a judge model, and a target model, then wires
// a full App via NewApp. The mock upstream serves as BOTH the judge and the
// target so the gateway can end-to-end route + execute.
func newTestApp(t *testing.T, upstreamURL string) *testApp {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	key := store.DeriveKey("test-seed")
	prov := &store.Provider{Name: "p", BaseURL: upstreamURL, APIKey: store.Encrypt(key, "sk-test"), Protocol: "openai", Enabled: true}
	if err := st.CreateProvider(prov); err != nil {
		t.Fatal(err)
	}
	judge := &store.Model{Name: "judge-mini", DisplayName: "Judge", ProviderID: prov.ID, Enabled: true}
	if err := st.CreateModel(judge); err != nil {
		t.Fatal(err)
	}
	if err := st.SetJudgeModel(judge.ID); err != nil {
		t.Fatal(err)
	}
	target := &store.Model{Name: "gpt-4o", DisplayName: "GPT4o", ProviderID: prov.ID, Enabled: true}
	if err := st.CreateModel(target); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateRoutingConfig(&store.RoutingConfig{
		ID:                       1,
		JudgeModelID:             &judge.ID,
		DefaultModelID:           &target.ID,
		EnableNextModelDirective: true,
		SessionTTLSeconds:        1800,
		JudgeMaxInputChars:       2000,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{}
	app := NewApp(cfg, st, key, "gw-token", "admin-token")
	return &testApp{App: app, UpstreamURL: upstreamURL}
}
