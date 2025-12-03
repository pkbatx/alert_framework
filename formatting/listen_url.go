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

	ext := os.Getenv("EXTERNAL_LISTEN_BASE_URL")
	if ext != "" {
		ext = strings.TrimRight(ext, "/")
		return ext + "/" + safeName
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
