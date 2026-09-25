package asnlookup

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Refresh policy defaults. A tick is a conditional ETag request, so an
// unchanged upstream file costs a single small response.
const (
	// DefaultUpdateInterval is the cadence of the automatic freshness check.
	DefaultUpdateInterval = 8 * time.Hour
	// minRetryInterval is the shortest wait after a failed check.
	minRetryInterval = 15 * time.Minute
	// maxRetryInterval caps the backoff ladder.
	maxRetryInterval = 2 * time.Hour
	// updateJitterFraction spreads ticks by +/-10 % so many instances do not
	// hit the endpoint in lockstep.
	updateJitterFraction = 0.10
)

// ASNLookup holds the current ASN database snapshot for a long-running
// process and can refresh it at runtime.
//
// The snapshot is immutable: refreshing parses a brand-new *ASNDatabase and
// swaps a pointer, so lookups running on other goroutines always observe
// either the complete old database or the complete new one, and never block.
// An *ASNEntry returned by LookupASN keeps pointing at the snapshot it came
// from and stays valid after a swap.
//
// Callers that read the exported IPv4Entries/IPv6Entries fields should call
// Database() per operation rather than caching the result, otherwise they keep
// reading a superseded snapshot.
type ASNLookup struct {
	cacheFile string
	url       string
	client    *http.Client

	snapshot atomic.Pointer[ASNDatabase]

	updateMu sync.Mutex // serialises UpdateDatabase

	maxCacheAge time.Duration

	schedMu    sync.Mutex
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	interval   time.Duration
	autoUpdate bool

	stateMu    sync.Mutex
	lastUpdate time.Time
	lastError  error
	failures   int

	// OnUpdate is invoked with the new snapshot whenever the database changed.
	OnUpdate func(*ASNDatabase)
	// OnError is invoked with any error from a refresh. Errors are never fatal:
	// the previous snapshot keeps serving lookups.
	OnError func(error)
}

// Option configures an ASNLookup.
type Option func(*ASNLookup)

// WithHTTPClient sets the HTTP client used for downloads.
func WithHTTPClient(client *http.Client) Option {
	return func(l *ASNLookup) {
		if client != nil {
			l.client = client
		}
	}
}

// WithURL overrides the upstream database URL.
func WithURL(url string) Option {
	return func(l *ASNLookup) {
		if url != "" {
			l.url = url
		}
	}
}

// WithOnUpdate registers a callback invoked after the snapshot was replaced.
func WithOnUpdate(fn func(*ASNDatabase)) Option {
	return func(l *ASNLookup) {
		l.OnUpdate = fn
	}
}

// WithOnError registers a callback invoked when a refresh fails.
func WithOnError(fn func(error)) Option {
	return func(l *ASNLookup) {
		l.OnError = fn
	}
}

// WithAutoUpdate enables or disables the automatic background refresh that
// NewASNLookup starts by default. Short-lived processes should disable it.
func WithAutoUpdate(enabled bool) Option {
	return func(l *ASNLookup) {
		l.autoUpdate = enabled
	}
}

// WithUpdateInterval sets the cadence of the automatic refresh. The default
// is DefaultUpdateInterval.
func WithUpdateInterval(interval time.Duration) Option {
	return func(l *ASNLookup) {
		if interval > 0 {
			l.interval = interval
		}
	}
}

// WithMaxCacheAge makes the constructor treat an existing cache file older
// than age as stale and refresh it up front. Zero means any existing cache
// file is accepted as is.
func WithMaxCacheAge(age time.Duration) Option {
	return func(l *ASNLookup) {
		l.maxCacheAge = age
	}
}

// NewASNLookup loads the ASN database from cacheFile, downloading it when the
// cache is missing or stale, and returns a holder that keeps it fresh.
//
// A background worker is started by default and performs an ETag-conditional
// freshness check every DefaultUpdateInterval (8 h, jittered by +/-10 %). It
// fails open: a failed check keeps the current snapshot and retries on a soft
// capped backoff. Call CancelScheduledUpdate on shutdown, or pass
// WithAutoUpdate(false) for a short-lived process.
//
// If the initial download fails but a cache file exists, the stale cache is
// used and the worker retries in the background; an error is returned only
// when there is no usable data at all.
func NewASNLookup(cacheFile string, opts ...Option) (*ASNLookup, error) {
	l := &ASNLookup{
		cacheFile:  cacheFile,
		url:        ASN_DATABASE_URL,
		client:     defaultHTTPClient(),
		interval:   DefaultUpdateInterval,
		autoUpdate: true,
	}
	for _, opt := range opts {
		opt(l)
	}

	db, err := l.initialLoad()
	if err != nil {
		return nil, err
	}
	l.snapshot.Store(db)

	if l.autoUpdate {
		if err := l.ScheduleUpdateDatabase(l.interval); err != nil {
			return nil, err
		}
	}

	return l, nil
}

// initialLoad builds the first snapshot, preferring a usable cache file over
// a failed download so a startup without network still serves stale data.
func (l *ASNLookup) initialLoad() (*ASNDatabase, error) {
	if l.cacheUsable() {
		if db, err := loadFromCache(l.cacheFile); err == nil {
			l.recordSuccess()
			return db, nil
		}
	}

	_, downloadErr := downloadASNDatabase(l.cacheFile, l.url, l.client)
	if downloadErr == nil {
		db, err := loadFromCache(l.cacheFile)
		if err == nil {
			l.recordSuccess()
			return db, nil
		}
		downloadErr = err
	}

	// Fail open: fall back to whatever cache file is on disk, however stale.
	if db, err := loadFromCache(l.cacheFile); err == nil {
		l.recordError(downloadErr)
		return db, nil
	}

	return nil, downloadErr
}

// cacheUsable reports whether the existing cache file may be used without a
// refresh, honouring the WithMaxCacheAge threshold.
func (l *ASNLookup) cacheUsable() bool {
	info, err := os.Stat(l.cacheFile)
	if err != nil {
		return false
	}
	if l.maxCacheAge <= 0 {
		return true
	}
	return time.Since(info.ModTime()) < l.maxCacheAge
}

// Database returns the current snapshot. Call it per operation: a snapshot
// obtained earlier keeps serving the data it was loaded with.
func (l *ASNLookup) Database() *ASNDatabase {
	return l.snapshot.Load()
}

// LookupASN resolves ip against the current snapshot.
func (l *ASNLookup) LookupASN(ip net.IP) ASNEntryInterface {
	db := l.snapshot.Load()
	if db == nil {
		return nil
	}
	return db.LookupASN(ip)
}

// GetPrefixesByASN returns the prefixes announced by asn in the current snapshot.
func (l *ASNLookup) GetPrefixesByASN(asn int) []netip.Prefix {
	db := l.snapshot.Load()
	if db == nil {
		return []netip.Prefix{}
	}
	return db.GetPrefixesByASN(asn)
}

// UpdateDatabase performs a conditional download and, when the upstream file
// changed, parses it and swaps in the new snapshot. It reports whether the
// snapshot was replaced. On failure the current snapshot is left untouched.
//
// Concurrent calls are serialised so the database is never downloaded twice
// at the same time.
func (l *ASNLookup) UpdateDatabase() (bool, error) {
	l.updateMu.Lock()
	defer l.updateMu.Unlock()

	return l.updateLocked()
}

// updateLocked performs the refresh. The caller must hold updateMu.
func (l *ASNLookup) updateLocked() (bool, error) {
	changed, err := downloadASNDatabase(l.cacheFile, l.url, l.client)
	if err != nil {
		l.recordError(err)
		return false, err
	}
	if !changed {
		l.recordSuccess()
		return false, nil
	}

	db, err := loadFromCache(l.cacheFile)
	if err != nil {
		l.recordError(err)
		return false, err
	}

	l.snapshot.Store(db)
	l.recordSuccess()

	if l.OnUpdate != nil {
		l.OnUpdate(db)
	}

	return true, nil
}

// LastUpdate returns the time of the last successful freshness check.
func (l *ASNLookup) LastUpdate() time.Time {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	return l.lastUpdate
}

// LastError returns the error of the most recent failed refresh, or nil when
// the last attempt succeeded.
func (l *ASNLookup) LastError() error {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	return l.lastError
}

func (l *ASNLookup) recordSuccess() {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	l.lastUpdate = time.Now()
	l.lastError = nil
	l.failures = 0
}

func (l *ASNLookup) recordError(err error) {
	l.stateMu.Lock()
	l.lastError = err
	l.failures++
	l.stateMu.Unlock()

	if l.OnError != nil {
		l.OnError(err)
	}
}

// ScheduleUpdateDatabase starts a background worker that checks for a new
// database every interval. Calling it again cancels the previous schedule
// first, so at most one worker is ever running.
//
// The worker fails open: a failed check keeps the current snapshot, reports
// the error through OnError/LastError and retries on a soft capped backoff
// (15 min, 30 min, 1 h, 2 h) instead of stopping the schedule.
func (l *ASNLookup) ScheduleUpdateDatabase(interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("update interval must be positive, got %s", interval)
	}

	l.CancelScheduledUpdate()

	l.schedMu.Lock()
	defer l.schedMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.interval = interval

	l.wg.Add(1)
	go l.runWorker(ctx)

	return nil
}

// CancelScheduledUpdate stops the background worker and waits for it to exit.
// It is safe to call when nothing is scheduled and safe to call repeatedly.
func (l *ASNLookup) CancelScheduledUpdate() {
	l.schedMu.Lock()
	cancel := l.cancel
	l.cancel = nil
	l.schedMu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	l.wg.Wait()
}

// runWorker re-arms a timer after every tick, because the delay varies with
// the backoff ladder.
func (l *ASNLookup) runWorker(ctx context.Context) {
	defer l.wg.Done()

	timer := time.NewTimer(l.nextDelay())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		l.tick()

		select {
		case <-ctx.Done():
			return
		default:
		}
		timer.Reset(l.nextDelay())
	}
}

// tick runs one refresh attempt. An update already in flight means the tick
// is skipped rather than queued, and every error is swallowed here: the
// schedule must survive it.
func (l *ASNLookup) tick() {
	if !l.updateMu.TryLock() {
		return
	}
	defer l.updateMu.Unlock()

	_, _ = l.updateLocked()
}

// nextDelay returns the wait before the next check: the jittered interval
// after a success, or the soft capped backoff ladder after consecutive
// failures.
func (l *ASNLookup) nextDelay() time.Duration {
	l.stateMu.Lock()
	failures := l.failures
	l.stateMu.Unlock()

	l.schedMu.Lock()
	interval := l.interval
	l.schedMu.Unlock()

	if interval <= 0 {
		interval = DefaultUpdateInterval
	}

	if failures == 0 {
		return jitter(interval)
	}

	delay := minRetryInterval
	for i := 1; i < failures && delay < maxRetryInterval; i++ {
		delay *= 2
	}
	if delay > maxRetryInterval {
		delay = maxRetryInterval
	}
	return delay
}

// jitter spreads d by +/-updateJitterFraction.
func jitter(d time.Duration) time.Duration {
	spread := float64(d) * updateJitterFraction
	offset := (rand.Float64()*2 - 1) * spread
	jittered := time.Duration(float64(d) + offset)
	if jittered < time.Millisecond {
		jittered = time.Millisecond
	}
	return jittered
}

// ensure the holder keeps satisfying both interfaces.
var (
	_ ASNLookuper          = (*ASNLookup)(nil)
	_ ASNDatabaseInterface = (*ASNLookup)(nil)
)
