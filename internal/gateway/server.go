package gateway

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1/evm_indexerv1connect"
)

// StartGateway serves the replicated Connect API (typed forwarding) plus a
// reverse proxy for everything else — the web UI at "/" and the OAuth callback —
// which is instance-agnostic and goes to any RUNNING instance. Blocks.
func StartGateway(db *evmi_database.EvmiDatabase, port uint64, ttl time.Duration, logger zerolog.Logger) {
	g := NewGateway(db, logger, ttl)

	mux := http.NewServeMux()

	// The Connect service (more specific than "/") is typed-forwarded per request.
	path, handler := evm_indexerv1connect.NewEvmIndexerServiceHandler(g)
	mux.Handle(path, handler)

	// Everything else (web UI, /auth/oauth/callback) is served by any instance —
	// it only depends on the shared DB. Proxy over HTTP/1.1 (instances' h2c
	// handler also speaks HTTP/1.1).
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			addr, err := g.resolver.AnyAddr()
			if err != nil {
				// Leave the host empty; the proxy returns a 502 the caller can see.
				logger.Error().Msg("gateway proxy: " + err.Error())
				return
			}
			r.URL.Scheme = "http"
			r.URL.Host = addr
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "gateway: no backend available: "+err.Error(), http.StatusBadGateway)
		},
	}
	mux.Handle("/", proxy)

	corsHandler := cors.AllowAll().Handler(mux)
	addr := "0.0.0.0:" + fmt.Sprint(port)
	logger.Info().Msg("EVMI gateway listening on " + addr)
	// h2c so the Connect handler can serve HTTP/2 cleartext, like the instances.
	if err := http.ListenAndServe(addr, h2c.NewHandler(corsHandler, &http2.Server{})); err != nil {
		logger.Fatal().Msg("gateway server stopped: " + err.Error())
	}
}
