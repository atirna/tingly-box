package typ

import (
	"fmt"
	"strconv"
	"strings"
)

// NextFreeNumberedID returns the smallest "<prefix>N" (N >= 1) not present in
// existingIDs. Reusing the lowest free number — rather than always
// incrementing past the historical maximum — means an ID freed by deleting an
// entry ("p2", "t3", ...) is offered to the next one created, keeping IDs
// short and gap-free over the lifetime of a config. Shared by profile IDs
// (internal/server/config) and team slugs (internal/db).
func NextFreeNumberedID(prefix string, existingIDs []string) string {
	taken := make(map[int]bool, len(existingIDs))
	for _, id := range existingIDs {
		if n, ok := parseNumberedID(prefix, id); ok {
			taken[n] = true
		}
	}
	n := 1
	for taken[n] {
		n++
	}
	return fmt.Sprintf("%s%d", prefix, n)
}

// parseNumberedID extracts N from an id of the exact form "<prefix>N", N >= 1.
// Round-tripping the parsed number back through fmt.Sprintf guards against
// non-canonical lookalikes (e.g. "p01") being mistaken for a taken slot.
func parseNumberedID(prefix, id string) (int, bool) {
	numberText, ok := strings.CutPrefix(id, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(numberText)
	if err != nil || n <= 0 || fmt.Sprintf("%s%d", prefix, n) != id {
		return 0, false
	}
	return n, true
}
