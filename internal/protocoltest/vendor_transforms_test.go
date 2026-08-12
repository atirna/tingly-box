package protocoltest

import "testing"

// TestVendorTransforms drives cached and no-cache requests to virtual
// providers whose APIBase matches real vendor discriminators (api.openai.com,
// api.deepseek.com, ...), verifying ApplyProviderTransforms' explicit-
// prompt-cache allowlist end-to-end rather than against a hand-built request
// struct. The case implementation is shared with `harness matrix --mode=vendor`.
func TestVendorTransforms(t *testing.T) {
	m := DefaultMatrix()
	for _, fx := range vendorFixtures {
		fx := fx
		for _, streaming := range m.Streaming {
			streaming := streaming
			t.Run("vendor/"+fx.name+"/"+streamMode(streaming), func(t *testing.T) {
				t.Parallel()
				env := NewTestEnv(t)
				defer env.Close()
				runVendorTransformCase(t, env, fx, streaming)
			})
		}
	}
}
