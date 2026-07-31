package protocoltest

import "testing"

// TestCacheControls drives every direct and ABA cache/no-cache request through
// the real gateway. The case implementation is shared with
// `harness matrix --mode=cache_controls`.
func TestCacheControls(t *testing.T) {
	m := DefaultMatrix()
	for _, pair := range m.Pairs {
		pair := pair
		for _, streaming := range m.Streaming {
			streaming := streaming
			t.Run("single/"+pair.String()+"/"+streamMode(streaming), func(t *testing.T) {
				t.Parallel()
				env := NewTestEnv(t)
				defer env.Close()
				runSingleCacheControlCase(t, env, pair, streaming)
			})
		}
	}

	for _, ic := range DefaultIdempotentCases() {
		ic := ic
		for _, streaming := range m.Streaming {
			streaming := streaming
			t.Run("aba/"+ic.Name+"/"+streamMode(streaming), func(t *testing.T) {
				t.Parallel()
				env := NewTestEnv(t)
				defer env.Close()
				runABACacheControlCase(t, env, ic, streaming)
			})
		}
	}
}
