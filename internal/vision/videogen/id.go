package videogen

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Gateway job ids embed the serving provider so the later GET /videos/{id}
// and GET /videos/{id}/content requests can be routed back to the same
// upstream without any server-side job store. The id is opaque to callers and
// survives gateway restarts because it carries all routing state itself.
//
// Shape: "tbv_" + base64url(providerUUID + "|" + nativeID), unpadded.
const jobIDPrefix = "tbv_"

// EncodeJobID wraps an upstream-native job id with the provider that owns it.
func EncodeJobID(providerUUID, nativeID string) string {
	payload := providerUUID + "|" + nativeID
	return jobIDPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeJobID splits a gateway job id back into the provider UUID and the
// upstream-native job id. It rejects ids that were not minted by EncodeJobID
// so handlers can return a clean invalid-request error instead of forwarding
// garbage upstream.
func DecodeJobID(id string) (providerUUID, nativeID string, err error) {
	encoded, ok := strings.CutPrefix(id, jobIDPrefix)
	if !ok {
		return "", "", fmt.Errorf("videogen: job id %q was not issued by this gateway", id)
	}
	raw, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
	if decodeErr != nil {
		return "", "", fmt.Errorf("videogen: malformed job id %q: %w", id, decodeErr)
	}
	providerUUID, nativeID, ok = strings.Cut(string(raw), "|")
	if !ok || providerUUID == "" || nativeID == "" {
		return "", "", fmt.Errorf("videogen: malformed job id %q", id)
	}
	return providerUUID, nativeID, nil
}
