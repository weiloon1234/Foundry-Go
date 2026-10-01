// Package websocket provides a bounded protocol client for independent consumer
// tests. Dial performs real HTTP/WebSocket I/O; this is not a production SDK.
package websocket

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"sync"

	transport "github.com/coder/websocket"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/secret"
	protocol "github.com/weiloon1234/Foundry-Go/websocket"
)

// Client permits one reader and serializes writes. A canceled I/O context closes
// the transport, following the underlying WebSocket connection contract.
type Client struct {
	socket *transport.Conn
	limit  int
	write  sync.Mutex
}

func Dial(ctx context.Context, url string, headers stdhttp.Header, maxFrameBytes int) (*Client, error) {
	return dial(ctx, url, headers, maxFrameBytes, protocol.Subprotocol)
}

// DialTicket performs a browser-style handshake: the single-use ticket travels
// as one extra Sec-WebSocket-Protocol entry, never in the URL.
func DialTicket(ctx context.Context, url string, headers stdhttp.Header, maxFrameBytes int, ticket secret.String) (*Client, error) {
	if ticket.IsZero() {
		return nil, fault.New(fault.Invalid, "WebSocket test ticket is empty")
	}
	return dial(ctx, url, headers, maxFrameBytes, protocol.Subprotocol, protocol.TicketSubprotocolPrefix+ticket.Reveal())
}

func dial(ctx context.Context, url string, headers stdhttp.Header, maxFrameBytes int, subprotocols ...string) (*Client, error) {
	if ctx == nil || maxFrameBytes < 1 || maxFrameBytes > 1<<20 {
		return nil, fault.New(fault.Invalid, "invalid WebSocket test client configuration")
	}
	socket, response, err := transport.Dial(ctx, url, &transport.DialOptions{HTTPHeader: headers.Clone(), Subprotocols: subprotocols, CompressionMode: transport.CompressionDisabled})
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, err
	}
	if socket.Subprotocol() != protocol.Subprotocol {
		socket.CloseNow()
		return nil, fault.New(fault.Invalid, "server did not negotiate the Foundry protocol")
	}
	socket.SetReadLimit(int64(maxFrameBytes))
	return &Client{socket: socket, limit: maxFrameBytes}, nil
}
func (c *Client) Close() error {
	if c == nil || c.socket == nil {
		return nil
	}
	return c.socket.CloseNow()
}
func (c *Client) Send(ctx context.Context, request protocol.Request) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return c.SendText(ctx, data)
}

// SendText intentionally permits malformed protocol data for rejection tests.
func (c *Client) SendText(ctx context.Context, data []byte) error {
	if ctx == nil || c == nil || c.socket == nil || len(data) > c.limit {
		return fault.New(fault.Invalid, "test frame exceeds its bound")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.write.Lock()
	defer c.write.Unlock()
	return c.socket.Write(ctx, transport.MessageText, data)
}
func (c *Client) Receive(ctx context.Context) (protocol.Response, error) {
	if ctx == nil || c == nil || c.socket == nil {
		return protocol.Response{}, fault.New(fault.Invalid, "test client is not connected")
	}
	if err := ctx.Err(); err != nil {
		return protocol.Response{}, err
	}
	kind, data, err := c.socket.Read(ctx)
	if err != nil {
		return protocol.Response{}, err
	}
	if kind != transport.MessageText {
		return protocol.Response{}, fault.New(fault.Invalid, "unexpected binary protocol frame")
	}
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: c.limit, Depth: jsonwire.MaxDepth, Nodes: 65536}); err != nil {
		return protocol.Response{}, err
	}
	var result protocol.Response
	if err := json.Unmarshal(data, &result); err != nil {
		return protocol.Response{}, err
	}
	if result.Version != protocol.ProtocolVersion {
		return protocol.Response{}, fault.New(fault.Invalid, "unexpected server protocol version")
	}
	return result, nil
}
