package neural

import (
	"net/http"
	"time"
)

// newNeuralHTTPClient constructs an HTTP client with persistent keep-alive pooling
// and DisableCompression=true for local inference daemons.
func newNeuralHTTPClient(timeout time.Duration, maxIdle, maxPerHost int) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        maxIdle,
			MaxIdleConnsPerHost: maxPerHost,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		},
	}
}
