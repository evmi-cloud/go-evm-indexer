package gateway

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/rs/zerolog"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	v1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
	"github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1/evm_indexerv1connect"
)

// Gateway must implement the full Connect service surface (it replicates it).
var _ evm_indexerv1connect.EvmIndexerServiceHandler = (*Gateway)(nil)

// defaultCacheTTL bounds how long a location lookup is cached before the DB is
// consulted again.
const defaultCacheTTL = 30 * time.Second

// Gateway implements evm_indexerv1connect.EvmIndexerServiceHandler by resolving,
// per request, the instance that owns the addressed data and forwarding the call
// there. It never touches log data itself; it only routes.
type Gateway struct {
	resolver *Resolver
	pool     *ClientPool
	logger   zerolog.Logger
}

func NewGateway(db *evmi_database.EvmiDatabase, logger zerolog.Logger, ttl time.Duration) *Gateway {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &Gateway{
		resolver: NewResolver(db, ttl),
		pool:     NewClientPool(),
		logger:   logger,
	}
}

// --- routing: resolve the backend client for a request ---

func routeErr(err error) error {
	return connect.NewError(connect.CodeUnavailable, err)
}

func (g *Gateway) anyClient() (Client, error) {
	addr, err := g.resolver.AnyAddr()
	if err != nil {
		return nil, routeErr(err)
	}
	return g.pool.ForAddr(addr), nil
}

func (g *Gateway) clientForInstance(id uint) (Client, error) {
	addr, err := g.resolver.AddrForInstance(id)
	if err != nil {
		return nil, routeErr(err)
	}
	return g.pool.ForAddr(addr), nil
}

func (g *Gateway) clientForPipeline(id uint) (Client, error) {
	addr, err := g.resolver.AddrForPipeline(id)
	if err != nil {
		return nil, routeErr(err)
	}
	return g.pool.ForAddr(addr), nil
}

func (g *Gateway) clientForSource(id uint) (Client, error) {
	addr, err := g.resolver.AddrForSource(id)
	if err != nil {
		return nil, routeErr(err)
	}
	return g.pool.ForAddr(addr), nil
}

func (g *Gateway) clientForExporter(id uint) (Client, error) {
	addr, err := g.resolver.AddrForExporter(id)
	if err != nil {
		return nil, routeErr(err)
	}
	return g.pool.ForAddr(addr), nil
}

// copyAuth forwards the caller's bearer token to the backend (which enforces
// authentication against the shared DB); the gateway itself is a pass-through.
func copyAuth(from, to http.Header) {
	if a := from.Get("Authorization"); a != "" {
		to.Set("Authorization", a)
	}
}

// forward re-wraps the incoming request (message + auth header) and invokes the
// resolved backend's typed method. Generic over request/response types so every
// unary handler is a one-liner.
func forward[Req any, Resp any](
	ctx context.Context,
	in *connect.Request[Req],
	call func(context.Context, *connect.Request[Req]) (*connect.Response[Resp], error),
) (*connect.Response[Resp], error) {
	out := connect.NewRequest(in.Msg)
	copyAuth(in.Header(), out.Header())
	return call(ctx, out)
}

// --- server-streaming: pump a single backend, or fan out across the fleet ---

// pumpStream forwards every item from one backend stream to the downstream one.
func pumpStream[Item any](s *connect.ServerStreamForClient[Item], send func(*Item) error) error {
	defer s.Close()
	for s.Receive() {
		if err := send(s.Msg()); err != nil {
			return err
		}
	}
	return s.Err()
}

// fanoutStream merges the streams from several backends into the downstream one.
// A failing/absent backend is skipped (logged by the caller); the stream ends
// when the client disconnects or all backends finish.
func fanoutStream[Item any](
	ctx context.Context,
	addrs []string,
	open func(ctx context.Context, addr string) (*connect.ServerStreamForClient[Item], error),
	send func(*Item) error,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan *Item, 64)
	var wg sync.WaitGroup
	for _, addr := range addrs {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			s, err := open(ctx, addr)
			if err != nil {
				return
			}
			defer s.Close()
			for s.Receive() {
				select {
				case ch <- s.Msg():
				case <-ctx.Done():
					return
				}
			}
		}(addr)
	}
	go func() { wg.Wait(); close(ch) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-ch:
			if !ok {
				return nil
			}
			if err := send(item); err != nil {
				return err
			}
		}
	}
}

// StreamEvmLogSourceUpdates routes to the pipeline's instance when a pipeline is
// given, otherwise fans out across the fleet so a client watching "all" pipelines
// sees updates from every instance.
func (g *Gateway) StreamEvmLogSourceUpdates(
	ctx context.Context,
	req *connect.Request[v1.StreamEvmLogSourceUpdatesRequest],
	stream *connect.ServerStream[v1.EvmLogSource],
) error {
	open := func(ctx context.Context, addr string) (*connect.ServerStreamForClient[v1.EvmLogSource], error) {
		out := connect.NewRequest(req.Msg)
		copyAuth(req.Header(), out.Header())
		return g.pool.ForAddr(addr).StreamEvmLogSourceUpdates(ctx, out)
	}

	if pid := uint(req.Msg.GetPipelineId()); pid != 0 {
		addr, err := g.resolver.AddrForPipeline(pid)
		if err != nil {
			return routeErr(err)
		}
		s, err := open(ctx, addr)
		if err != nil {
			return err
		}
		return pumpStream(s, stream.Send)
	}

	addrs, err := g.resolver.AllAddrs()
	if err != nil {
		return routeErr(err)
	}
	return fanoutStream(ctx, addrs, open, stream.Send)
}

// InstallPlugin fans out to every RUNNING instance instead of routing to one:
// installing builds the plugin's shared object on that instance's local disk, so
// each instance that might run an exporter using the plugin needs its own build.
// Installs run concurrently; the aggregate succeeds only if every instance
// succeeds, and the error lists the instances that failed. A per-instance call
// error and a per-instance Success=false are both treated as failures.
func (g *Gateway) InstallPlugin(ctx context.Context, req *connect.Request[v1.InstallPluginRequest]) (*connect.Response[v1.InstallPluginResponse], error) {
	addrs, err := g.resolver.AllAddrs()
	if err != nil {
		return nil, routeErr(err)
	}

	outcomes := make([]installOutcome, len(addrs))
	var wg sync.WaitGroup
	for i, addr := range addrs {
		wg.Add(1)
		go func(i int, addr string) {
			defer wg.Done()
			out := connect.NewRequest(req.Msg)
			copyAuth(req.Header(), out.Header())
			resp, err := g.pool.ForAddr(addr).InstallPlugin(ctx, out)
			switch {
			case err != nil:
				outcomes[i] = installOutcome{addr: addr, fail: err.Error()}
			case !resp.Msg.GetSuccess():
				outcomes[i] = installOutcome{addr: addr, status: resp.Msg.GetStatus(), fail: resp.Msg.GetError()}
			default:
				outcomes[i] = installOutcome{addr: addr, status: resp.Msg.GetStatus()}
			}
		}(i, addr)
	}
	wg.Wait()

	resp := aggregateInstall(outcomes)
	if resp.GetSuccess() {
		g.logger.Info().Msg("gateway InstallPlugin: installed on " + strings.Join(addrs, ", "))
	} else {
		g.logger.Error().Msg("gateway InstallPlugin: " + resp.GetError())
	}
	return connect.NewResponse(resp), nil
}

type installOutcome struct {
	addr   string
	status string
	fail   string // non-empty when this instance failed
}

// aggregateInstall combines per-instance install outcomes: success only if every
// instance succeeded; the error lists each failing instance.
func aggregateInstall(outcomes []installOutcome) *v1.InstallPluginResponse {
	var failures []string
	status := ""
	for _, o := range outcomes {
		if o.status != "" {
			status = o.status
		}
		if o.fail != "" {
			failures = append(failures, o.addr+": "+o.fail)
		}
	}
	resp := &v1.InstallPluginResponse{Success: len(failures) == 0, Status: status}
	if len(failures) > 0 {
		resp.Error = "install failed on " + strings.Join(failures, "; ")
	}
	return resp
}

// StreamEvmiExporterUpdates mirrors StreamEvmLogSourceUpdates for exporters.
func (g *Gateway) StreamEvmiExporterUpdates(
	ctx context.Context,
	req *connect.Request[v1.StreamEvmiExporterUpdatesRequest],
	stream *connect.ServerStream[v1.EvmiExporter],
) error {
	open := func(ctx context.Context, addr string) (*connect.ServerStreamForClient[v1.EvmiExporter], error) {
		out := connect.NewRequest(req.Msg)
		copyAuth(req.Header(), out.Header())
		return g.pool.ForAddr(addr).StreamEvmiExporterUpdates(ctx, out)
	}

	if pid := uint(req.Msg.GetPipelineId()); pid != 0 {
		addr, err := g.resolver.AddrForPipeline(pid)
		if err != nil {
			return routeErr(err)
		}
		s, err := open(ctx, addr)
		if err != nil {
			return err
		}
		return pumpStream(s, stream.Send)
	}

	addrs, err := g.resolver.AllAddrs()
	if err != nil {
		return routeErr(err)
	}
	return fanoutStream(ctx, addrs, open, stream.Send)
}
