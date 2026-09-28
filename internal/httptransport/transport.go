// Package httptransport owns native pooled transport construction shared by
// outbound clients and provider adapters. Callers retain their operation policy.
package httptransport

import (
	"net"
	"net/http"
	"time"
)

// Config is validated by the owning public boundary before construction.
type Config struct {
	ConnectTimeout         time.Duration
	RequestTimeout         time.Duration
	MaxIdleConnections     int
	MaxIdlePerHost         int
	MaxResponseHeaderBytes int64
}

func New(config Config) *http.Transport {
	return &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            (&net.Dialer{Timeout: config.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           config.MaxIdleConnections,
		MaxIdleConnsPerHost:    config.MaxIdlePerHost,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    config.ConnectTimeout,
		ResponseHeaderTimeout:  config.RequestTimeout,
		MaxResponseHeaderBytes: config.MaxResponseHeaderBytes,
	}
}
