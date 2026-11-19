package doubaoapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func NewRequestID() string {
	return uuid.NewString()
}

func NewConnectID() string {
	return uuid.NewString()
}

func NewASRHeaders(appKey, accessKey, resourceID, connectID string) http.Header {
	reqID := NewRequestID()
	if connectID == "" {
		connectID = reqID
	}
	header := http.Header{}
	header.Set("X-Api-Resource-Id", resourceID)
	header.Set("X-Api-Connect-Id", connectID)
	header.Set("X-Api-Request-Id", reqID)
	header.Set("X-Api-Access-Key", accessKey)
	header.Set("X-Api-App-Key", appKey)
	return header
}

func NewTTSHeaders(appID, accessKey, resourceID string) http.Header {
	return NewTTSHeadersWithAuth("", appID, accessKey, resourceID)
}

func NewTTSHeadersWithAuth(apiKey, appID, accessKey, resourceID string) http.Header {
	header := http.Header{}
	if trimmedKey := strings.TrimSpace(apiKey); trimmedKey != "" {
		header.Set("X-Api-Key", trimmedKey)
	} else {
		header.Set("Authorization", fmt.Sprintf("Bearer;%s", accessKey))
		header.Set("X-Api-App-Id", appID)
		header.Set("X-Api-Access-Key", accessKey)
	}
	header.Set("X-Api-Resource-Id", resourceID)
	header.Set("X-Api-Request-Id", NewRequestID())
	return header
}

func NewTTSWebsocketHeaders(appID, accessKey, resourceID, connectID string) http.Header {
	return NewTTSWebsocketHeadersWithAuth("", appID, accessKey, resourceID, connectID)
}

func NewTTSWebsocketHeadersWithAuth(apiKey, appID, accessKey, resourceID, connectID string) http.Header {
	header := NewTTSHeadersWithAuth(apiKey, appID, accessKey, resourceID)
	if connectID != "" {
		header.Set("X-Api-Connect-Id", connectID)
	}
	return header
}
