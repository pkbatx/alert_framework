package formatting

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// BuildListenURL constructs an externally accessible audio URL for webhooks.
func BuildListenURL(filename string) string {
	safeName := strings.TrimLeft(strings.TrimSpace(filename), "/")
	segments := strings.Split(safeName, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	safeName = strings.Join(segments, "/")

	sanitizeBase := func(raw string) string {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return ""
		}
		return strings.TrimRight(raw, "/")
	}

	if ext := sanitizeBase(os.Getenv("EXTERNAL_LISTEN_BASE_URL")); ext != "" {
		return ext + "/" + safeName
	}
	if public := sanitizeBase(os.Getenv("PUBLIC_BASE_URL")); public != "" {
		return public + "/" + safeName
	}

	port := strings.TrimSpace(os.Getenv("HTTP_PORT"))
	if strings.HasPrefix(port, ":") {
		port = strings.TrimPrefix(port, ":")
	}
	if port == "" {
		port = "8000"
	}
	return fmt.Sprintf("http://localhost:%s/%s", port, safeName)
}
