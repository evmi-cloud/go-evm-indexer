package grpc

import (
	"net/http"

	"github.com/mustafaturan/bus/v3"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1/evm_indexerv1connect"
)

type EvmIndexerServer struct {
	db     *evmi_database.EvmiDatabase
	bus    *bus.Bus
	logger zerolog.Logger
}

func StartGrpcServer(
	db *evmi_database.EvmiDatabase,
	bus *bus.Bus,
	logger zerolog.Logger,
) {
	indexer := &EvmIndexerServer{
		db:     db,
		bus:    bus,
		logger: logger,
	}

	mux := http.NewServeMux()

	path, handler := evm_indexerv1connect.NewEvmIndexerServiceHandler(indexer)
	mux.Handle(path, handler)

	//TODO: handle multiple TLS configurations
	corsHandler := cors.AllowAll().Handler(mux)
	http.ListenAndServe(
		"0.0.0.0:8080",
		// Use h2c so we can serve HTTP/2 without TLS.
		h2c.NewHandler(corsHandler, &http2.Server{}),
	)
}
