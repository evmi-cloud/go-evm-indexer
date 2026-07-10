package grpc

import (
	"net/http"

	"connectrpc.com/connect"
	"github.com/mustafaturan/bus/v3"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/evmi-cloud/go-evm-indexer/internal/auth"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1/evm_indexerv1connect"
)

type EvmIndexerServer struct {
	db     *evmi_database.EvmiDatabase
	bus    *bus.Bus
	auth   *auth.Authenticator
	logger zerolog.Logger
}

func StartGrpcServer(
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	logger zerolog.Logger,
) {
	authenticator := auth.NewAuthenticator(db)

	indexer := &EvmIndexerServer{
		db:     db,
		bus:    bus,
		auth:   authenticator,
		logger: logger,
	}

	mux := http.NewServeMux()

	// Every Connect RPC requires a valid bearer token, except the public auth
	// bootstrap procedures (login and obtaining the OAuth login URL).
	path, handler := evm_indexerv1connect.NewEvmIndexerServiceHandler(
		indexer,
		connect.WithInterceptors(authenticator.Interceptor(
			evm_indexerv1connect.EvmIndexerServiceLoginProcedure,
			evm_indexerv1connect.EvmIndexerServiceListOAuthLoginUrlsProcedure,
		)),
	)
	mux.Handle(path, handler)

	// Only the OAuth callback (a browser redirect target) stays on HTTP.
	auth.RegisterRoutes(mux, authenticator, logger)

	// Serve the built web UI at "/" (the API and auth patterns above are more
	// specific and take precedence). Skipped when no build is present.
	if webui := newWebUIHandler(webuiDir()); webui != nil {
		mux.Handle("/", webui)
		logger.Info().Msg("serving web UI from " + webuiDir())
	} else {
		logger.Info().Msg("no web UI build found at " + webuiDir() + "; static serving disabled")
	}

	//TODO: handle multiple TLS configurations
	corsHandler := cors.AllowAll().Handler(mux)
	http.ListenAndServe(
		"0.0.0.0:8080",
		// Use h2c so we can serve HTTP/2 without TLS.
		h2c.NewHandler(corsHandler, &http2.Server{}),
	)
}
