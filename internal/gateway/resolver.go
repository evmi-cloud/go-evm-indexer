// Package gateway implements a stateful load balancer in front of a fleet of
// EVMI instances. Each instance is stateful — it indexes specific pipelines and
// may keep their data in a local store — so a request must reach the instance
// that owns the data, not just any instance. The gateway replicates the Connect
// service, resolves the owning instance for each request by reading the shared
// metadata DB (with in-memory TTL caching so it doesn't spam the DB), and
// forwards the call to that instance's address (its EvmiInstance IpV4 + Port).
package gateway

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
)

// defaultInstancePort mirrors the hardcoded gRPC listen port used when an
// instance row predates the Port column (0).
const defaultInstancePort uint64 = 8080

type cacheEntry[V any] struct {
	val V
	exp time.Time
}

// ttlCache is a tiny concurrency-safe cache with per-entry expiry. It exists so
// location lookups hit the DB at most once per TTL per key.
type ttlCache[K comparable, V any] struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[K]cacheEntry[V]
}

func newTTLCache[K comparable, V any](ttl time.Duration) *ttlCache[K, V] {
	return &ttlCache[K, V]{ttl: ttl, m: make(map[K]cacheEntry[V])}
}

// getOrLoad returns the cached value if fresh, otherwise calls load (outside the
// lock) and caches the result. Errors are not cached.
func (c *ttlCache[K, V]) getOrLoad(key K, load func() (V, error)) (V, error) {
	c.mu.Lock()
	if e, ok := c.m[key]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.val, nil
	}
	c.mu.Unlock()

	val, err := load()
	if err != nil {
		var zero V
		return zero, err
	}

	c.mu.Lock()
	c.m[key] = cacheEntry[V]{val: val, exp: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return val, nil
}

// invalidate drops a key so the next lookup re-reads the DB (used when a forward
// to the cached address fails, e.g. the instance moved or restarted).
func (c *ttlCache[K, V]) invalidate(key K) {
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
}

// Resolver maps request entities to the address of the instance that owns them,
// reading the shared metadata DB and caching every hop.
type Resolver struct {
	db *evmi_database.EvmiDatabase

	pipelineToInstance *ttlCache[uint, uint]
	sourceToPipeline   *ttlCache[uint, uint]
	exporterToPipeline *ttlCache[uint, uint]
	instanceToAddr     *ttlCache[uint, string]

	// "any" routing: the set of RUNNING instance addresses, refreshed on a TTL,
	// consumed round-robin. Used for instance-agnostic (shared-DB) RPCs.
	anyMu    sync.Mutex
	anyAddrs []string
	anyExp   time.Time
	anyTTL   time.Duration
	rr       uint64
}

func NewResolver(db *evmi_database.EvmiDatabase, ttl time.Duration) *Resolver {
	return &Resolver{
		db:                 db,
		pipelineToInstance: newTTLCache[uint, uint](ttl),
		sourceToPipeline:   newTTLCache[uint, uint](ttl),
		exporterToPipeline: newTTLCache[uint, uint](ttl),
		instanceToAddr:     newTTLCache[uint, string](ttl),
		anyTTL:             ttl,
	}
}

func instanceAddr(inst evmi_database.EvmiInstance) string {
	port := inst.Port
	if port == 0 {
		port = defaultInstancePort
	}
	host := inst.IpV4
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// AddrForInstance resolves an instance id to its "host:port".
func (r *Resolver) AddrForInstance(id uint) (string, error) {
	if id == 0 {
		return "", errors.New("no instance id")
	}
	return r.instanceToAddr.getOrLoad(id, func() (string, error) {
		var inst evmi_database.EvmiInstance
		if err := r.db.Conn.First(&inst, id).Error; err != nil {
			return "", fmt.Errorf("instance %d not found: %w", id, err)
		}
		return instanceAddr(inst), nil
	})
}

func (r *Resolver) instanceForPipeline(id uint) (uint, error) {
	if id == 0 {
		return 0, errors.New("no pipeline id")
	}
	return r.pipelineToInstance.getOrLoad(id, func() (uint, error) {
		var p evmi_database.EvmLogPipeline
		if err := r.db.Conn.First(&p, id).Error; err != nil {
			return 0, fmt.Errorf("pipeline %d not found: %w", id, err)
		}
		if p.EvmiInstanceID == 0 {
			return 0, fmt.Errorf("pipeline %d is not bound to an instance", id)
		}
		return p.EvmiInstanceID, nil
	})
}

func (r *Resolver) instanceForSource(id uint) (uint, error) {
	if id == 0 {
		return 0, errors.New("no source id")
	}
	pipelineID, err := r.sourceToPipeline.getOrLoad(id, func() (uint, error) {
		var s evmi_database.EvmLogSource
		if err := r.db.Conn.First(&s, id).Error; err != nil {
			return 0, fmt.Errorf("source %d not found: %w", id, err)
		}
		return s.EvmLogPipelineID, nil
	})
	if err != nil {
		return 0, err
	}
	return r.instanceForPipeline(pipelineID)
}

func (r *Resolver) instanceForExporter(id uint) (uint, error) {
	if id == 0 {
		return 0, errors.New("no exporter id")
	}
	pipelineID, err := r.exporterToPipeline.getOrLoad(id, func() (uint, error) {
		var e evmi_database.EvmiExporter
		if err := r.db.Conn.First(&e, id).Error; err != nil {
			return 0, fmt.Errorf("exporter %d not found: %w", id, err)
		}
		return e.EvmLogPipelineID, nil
	})
	if err != nil {
		return 0, err
	}
	return r.instanceForPipeline(pipelineID)
}

// AddrForPipeline / AddrForSource / AddrForExporter resolve the owning instance's
// address for the given entity.
func (r *Resolver) AddrForPipeline(id uint) (string, error) {
	iid, err := r.instanceForPipeline(id)
	if err != nil {
		return "", err
	}
	return r.AddrForInstance(iid)
}

func (r *Resolver) AddrForSource(id uint) (string, error) {
	iid, err := r.instanceForSource(id)
	if err != nil {
		return "", err
	}
	return r.AddrForInstance(iid)
}

func (r *Resolver) AddrForExporter(id uint) (string, error) {
	iid, err := r.instanceForExporter(id)
	if err != nil {
		return "", err
	}
	return r.AddrForInstance(iid)
}

// runningAddrs returns the addresses of RUNNING instances (falling back to all
// instances if none are marked running), cached on a TTL.
func (r *Resolver) runningAddrs() ([]string, error) {
	r.anyMu.Lock()
	defer r.anyMu.Unlock()
	if len(r.anyAddrs) > 0 && time.Now().Before(r.anyExp) {
		return r.anyAddrs, nil
	}

	var insts []evmi_database.EvmiInstance
	if err := r.db.Conn.Where("status = ?", "RUNNING").Find(&insts).Error; err != nil {
		return nil, err
	}
	if len(insts) == 0 {
		// No instance is marked RUNNING (e.g. all just booted) — fall back to any.
		if err := r.db.Conn.Find(&insts).Error; err != nil {
			return nil, err
		}
	}
	if len(insts) == 0 {
		return nil, errors.New("no evmi instances registered")
	}

	addrs := make([]string, 0, len(insts))
	for _, in := range insts {
		addrs = append(addrs, instanceAddr(in))
	}
	r.anyAddrs = addrs
	r.anyExp = time.Now().Add(r.anyTTL)
	return addrs, nil
}

// AnyAddr picks a RUNNING instance address round-robin, for RPCs that operate on
// the shared metadata DB and can be served by any instance.
func (r *Resolver) AnyAddr() (string, error) {
	addrs, err := r.runningAddrs()
	if err != nil {
		return "", err
	}
	i := atomic.AddUint64(&r.rr, 1)
	return addrs[int(i)%len(addrs)], nil
}

// AllAddrs returns every RUNNING instance address, for fan-out (e.g. streaming
// updates across the whole fleet).
func (r *Resolver) AllAddrs() ([]string, error) {
	return r.runningAddrs()
}

// InvalidateInstance forgets a cached instance address (called after a failed
// forward so the next request re-resolves).
func (r *Resolver) InvalidateInstance(id uint) {
	r.instanceToAddr.invalidate(id)
	r.anyMu.Lock()
	r.anyExp = time.Time{}
	r.anyMu.Unlock()
}
