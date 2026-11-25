package chat

import (
	"net/url"
	"strings"
)

// SQLiteFileURI is the file: URI of a slash-separated path (filepath.ToSlash). A Windows path (C:/…) gets a leading
// slash, so the drive is part of the path and not read as the URI's host.
func SQLiteFileURI(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
