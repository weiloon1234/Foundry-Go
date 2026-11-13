package http

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeUploadBodyTimeoutThroughCompressionAndETags(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "http"
		if encrypted {
			name = "tls"
		}
		t.Run(name, func(t *testing.T) {
			pool := claimedUploadTestPool(t, 1, DefaultEndpointLimits().Multipart.Bytes)
			endpoint := admittedEndpoint(t, pool)
			endpoint.route = ConfigureRoute(endpoint.route, Timeout(300*time.Millisecond))
			var publications atomic.Int32
			router, err := NewRouter(endpoint.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				publications.Add(1)
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			handler, err := ApplyMiddleware(router, Compression(DefaultCompressionConfig()), ETags(DefaultETagConfig()))
			if err != nil {
				t.Fatal(err)
			}
			// Standard native hosting: no Foundry kernel, listener wrapper or
			// connection-context hook, including when TLS owns the connection.
			server := httptest.NewUnstartedServer(handler)
			server.Config.ReadTimeout, server.Config.WriteTimeout = 5*time.Second, 5*time.Second
			if encrypted {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			client := server.Client()
			client.Timeout = 2 * time.Second
			address := server.Listener.Addr().String()
			var conn net.Conn
			if encrypted {
				conn, err = tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, client.Transport.(*stdhttp.Transport).TLSClientConfig)
			} else {
				conn, err = net.DialTimeout("tcp", address, time.Second)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, primaryUpload(strings.Repeat("x", 64<<10)))
			started := time.Now()
			if _, err := fmt.Fprintf(conn, "POST /uploads HTTP/1.1\r\nHost: localhost\r\nAccept-Encoding: gzip\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s", media, len(wire), wire[:len(wire)/2]); err != nil {
				t.Fatal(err)
			}
			response, err := stdhttp.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal("native stalled-body timeout lost its response", err)
			}
			var failure ErrorResponse
			err = json.NewDecoder(response.Body).Decode(&failure)
			response.Body.Close()
			elapsed := time.Since(started)
			if err != nil || response.StatusCode != 408 || failure.Code != RequestTimeout || !response.Close {
				t.Fatal("native timeout did not deliver a closing 408", response.StatusCode, failure, err)
			}
			if elapsed < 200*time.Millisecond || elapsed >= 1500*time.Millisecond || publications.Load() != 0 {
				t.Fatal("native body deadline was not enforced before publication", elapsed, publications.Load())
			}
			until := time.Now().Add(time.Second)
			for {
				active, reserved := pool.Usage()
				if active == 0 && reserved == 0 {
					break
				}
				if time.Now().After(until) {
					t.Fatal("native timeout retained upload capacity", active, reserved)
				}
				time.Sleep(time.Millisecond)
			}
			healthy, media := multipartWire(t, primaryUpload("healthy"))
			response, err = client.Post(server.URL+"/uploads", media, bytes.NewReader(healthy))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 204 || publications.Load() != 1 {
				t.Fatal("native upload capacity did not recover", response.StatusCode, publications.Load())
			}
		})
	}
}
