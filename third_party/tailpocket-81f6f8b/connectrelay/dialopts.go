package connectrelay

import (
	"net/http"

	"github.com/coder/websocket"
)

func dialOptions(header http.Header, client *http.Client) *websocket.DialOptions {
	return &websocket.DialOptions{HTTPHeader: header, HTTPClient: client}
}
