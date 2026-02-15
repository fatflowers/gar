package anthropicprovider

import (
	"errors"
	"net"
	"net/http"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// isRetryableProviderError identifies transient transport/API failures worth retrying.
func isRetryableProviderError(err error) bool {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= http.StatusInternalServerError
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}
