package gateway

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"

	"golang.org/x/net/http2"

	"github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1/evm_indexerv1connect"
)

// Client is the Connect client interface for a single backend instance.
type Client = evm_indexerv1connect.EvmIndexerServiceClient

// h2cTransport dials plaintext HTTP/2 (h2c), matching how instances serve.
func h2cTransport() *http2.Transport {
	return &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

// ClientPool memoizes one Connect client per backend address (Connect clients are
// safe for concurrent use and pool their own connections).
type ClientPool struct {
	httpClient *http.Client
	mu         sync.Mutex
	clients    map[string]Client
}

func NewClientPool() *ClientPool {
	return &ClientPool{
		httpClient: &http.Client{Transport: h2cTransport()},
		clients:    make(map[string]Client),
	}
}

func (p *ClientPool) ForAddr(addr string) Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[addr]; ok {
		return c
	}
	c := evm_indexerv1connect.NewEvmIndexerServiceClient(p.httpClient, "http://"+addr)
	p.clients[addr] = c
	return c
}
