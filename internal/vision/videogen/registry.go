package videogen

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// New builds a native video generation Client for the given provider. It only
// serves vendors with a bespoke (non-OpenAI) video API — currently DashScope
// and MiniMax. The model argument is the already-routed upstream model id; it
// is not used for vendor selection (that is host-based) but adapters may read
// it.
//
// OpenAI providers are NOT served here: client.OpenAIClient forwards those to
// the SDK's native Videos service. New returns ErrUnsupported for them, which
// in practice signals a routing bug since the caller is expected to dispatch
// only DashScope / MiniMax here.
func New(provider *typ.Provider, model string) (Client, error) {
	if provider == nil {
		return nil, fmt.Errorf("videogen: nil provider")
	}

	vendor := DetectVendor(provider)
	logrus.Debugf("[videogen] provider %s (api_base=%s) detected vendor: %s", provider.Name, provider.APIBase, vendor)

	switch vendor {
	case VendorDashScope:
		return newDashScopeClient(provider)
	case VendorMinimax:
		return newMinimaxClient(provider)
	case VendorArk:
		return newArkClient(provider)
	default:
		return nil, fmt.Errorf("%w: provider %s (api_base=%s)", ErrUnsupported, provider.Name, provider.APIBase)
	}
}
