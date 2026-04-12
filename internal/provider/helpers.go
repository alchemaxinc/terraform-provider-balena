package provider

import (
	"strconv"
)

// parseID parses a string ID into an int64.
func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
