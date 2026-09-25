package asnlookup

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNewASNLookup_InitialDownload(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	db := lookup.Database()
	if db == nil {
		t.Fatal("expected a snapshot to be loaded")
	}
	if len(db.IPv4Entries) != 2 {
		t.Errorf("IPv4 entries = %d, want 2", len(db.IPv4Entries))
	}
	if entry := lookup.LookupASN(net.ParseIP("8.8.8.8")); entry == nil || entry.GetASN() != 15169 {
		t.Errorf("LookupASN(8.8.8.8) = %v, want AS15169", entry)
	}
	if lookup.LastUpdate().IsZero() {
		t.Error("expected LastUpdate to be set after a successful load")
	}
	if err := lookup.LastError(); err != nil {
		t.Errorf("LastError() = %v, want nil", err)
	}
}

func TestNewASNLookup_AutoUpdateDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	if !lookup.autoUpdate {
		t.Error("expected auto-update to be enabled by default")
	}
	if lookup.interval != DefaultUpdateInterval {
		t.Errorf("interval = %s, want %s", lookup.interval, DefaultUpdateInterval)
	}

	lookup.schedMu.Lock()
	scheduled := lookup.cancel != nil
	lookup.schedMu.Unlock()
	if !scheduled {
		t.Error("expected a worker to be scheduled by default")
	}
}

func TestNewASNLookup_AutoUpdateDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithAutoUpdate(false),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	lookup.schedMu.Lock()
	scheduled := lookup.cancel != nil
	lookup.schedMu.Unlock()
	if scheduled {
		t.Error("expected no worker when auto-update is disabled")
	}

	initial, _ := srv.counts()
	time.Sleep(50 * time.Millisecond)
	if requests, _ := srv.counts(); requests != initial {
		t.Errorf("requests = %d, want %d: no background traffic expected", requests, initial)
	}

	// Cancelling without a schedule must not panic.
	lookup.CancelScheduledUpdate()
	lookup.CancelScheduledUpdate()
}

func TestNewASNDatabaseWithCache_NoBackgroundWorker(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	writeCacheFixture(t, cacheFile, fixtureTSVv1)

	baseline := runtime.NumGoroutine()

	db, err := NewASNDatabaseWithCache(cacheFile)
	if err != nil {
		t.Fatalf("NewASNDatabaseWithCache() error = %v", err)
	}
	if len(db.IPv4Entries) != 2 {
		t.Errorf("IPv4 entries = %d, want 2", len(db.IPv4Entries))
	}

	if got := runtime.NumGoroutine(); got > baseline {
		t.Errorf("goroutines = %d, baseline %d: the legacy constructor must stay goroutine-free", got, baseline)
	}
}

func TestNewASNLookup_WithUpdateInterval(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithUpdateInterval(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	waitForRequests(t, srv, 4, 5*time.Second)
}

func TestNewASNLookup_ExistingCacheIsUsed(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	writeCacheFixture(t, cacheFile, fixtureTSVv1)
	srv := newFixtureServer(t, fixtureTSVv2, `"v2"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	if requests, _ := srv.counts(); requests != 0 {
		t.Errorf("requests = %d, want 0 when a cache file exists", requests)
	}
	if entry := lookup.LookupASN(net.ParseIP("9.9.9.9")); entry != nil {
		t.Error("expected the stale cache to be served, not the new fixture")
	}
}

func TestNewASNLookup_FallsBackToStaleCache(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	writeCacheFixture(t, cacheFile, fixtureTSVv1)
	srv := newFixtureServer(t, fixtureTSVv2, `"v2"`)
	srv.setStatus(http.StatusInternalServerError)

	// A zero max age would accept the cache without asking; force a refresh
	// attempt so the download failure is exercised.
	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithMaxCacheAge(1),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v, want a fail-open fallback", err)
	}
	if entry := lookup.LookupASN(net.ParseIP("8.8.8.8")); entry == nil {
		t.Error("expected the stale cache to keep serving lookups")
	}
	if lookup.LastError() == nil {
		t.Error("expected LastError to record the failed download")
	}
}

func TestNewASNLookup_NoUsableData(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)
	srv.setStatus(http.StatusInternalServerError)

	if _, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client())); err == nil {
		t.Fatal("expected an error when there is no cache and no download")
	}
}

func TestASNLookup_UpdateDatabaseSwapsSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	var updates int
	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithOnUpdate(func(*ASNDatabase) { updates++ }),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	if entry := lookup.LookupASN(net.ParseIP("9.9.9.9")); entry != nil {
		t.Fatal("9.9.9.9 should not resolve in the first fixture")
	}
	before := lookup.Database()

	srv.setBody(t, fixtureTSVv2, `"v2"`)

	changed, err := lookup.UpdateDatabase()
	if err != nil {
		t.Fatalf("UpdateDatabase() error = %v", err)
	}
	if !changed {
		t.Fatal("expected changed = true after the fixture changed")
	}
	if updates != 1 {
		t.Errorf("OnUpdate calls = %d, want 1", updates)
	}

	entry := lookup.LookupASN(net.ParseIP("9.9.9.9"))
	if entry == nil || entry.GetASN() != 19281 {
		t.Errorf("LookupASN(9.9.9.9) = %v, want AS19281", entry)
	}
	if lookup.Database() == before {
		t.Error("expected the snapshot pointer to be replaced")
	}
	// The previous snapshot stays valid and unchanged.
	if len(before.IPv4Entries) != 2 {
		t.Errorf("old snapshot IPv4 entries = %d, want 2", len(before.IPv4Entries))
	}
}

func TestASNLookup_UpdateDatabaseNotModified(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	before := lookup.Database()

	changed, err := lookup.UpdateDatabase()
	if err != nil {
		t.Fatalf("UpdateDatabase() error = %v", err)
	}
	if changed {
		t.Error("expected changed = false on a 304")
	}
	if lookup.Database() != before {
		t.Error("expected the snapshot to be untouched on a 304")
	}
}

func TestASNLookup_UpdateDatabaseKeepsSnapshotOnError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	var errs int
	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithOnError(func(error) { errs++ }),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	before := lookup.Database()

	srv.setStatus(http.StatusInternalServerError)

	changed, err := lookup.UpdateDatabase()
	if err == nil {
		t.Fatal("expected an error from a failing server")
	}
	if changed {
		t.Error("expected changed = false on error")
	}
	if lookup.Database() != before {
		t.Error("expected the snapshot to survive a failed update")
	}
	if errs != 1 {
		t.Errorf("OnError calls = %d, want 1", errs)
	}
	if lookup.LastError() == nil {
		t.Error("expected LastError to be set")
	}
	if entry := lookup.LookupASN(net.ParseIP("8.8.8.8")); entry == nil {
		t.Error("lookups must keep working after a failed update")
	}
}

func TestASNLookup_UpdateDatabaseKeepsSnapshotOnBadGzip(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	before := lookup.Database()

	srv.lock()
	srv.body = []byte("not gzipped at all")
	srv.etag = `"garbage"`
	srv.unlock()

	if _, err := lookup.UpdateDatabase(); err == nil {
		t.Fatal("expected a parse error for a non-gzip body")
	}
	if lookup.Database() != before {
		t.Error("expected the snapshot to survive an unparsable download")
	}
}

func TestASNLookup_ConcurrentLookupsDuringSwaps(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	const readers = 8
	done := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if entry := lookup.LookupASN(net.ParseIP("8.8.8.8")); entry == nil || entry.GetASN() != 15169 {
					t.Errorf("lookup returned %v during a swap, want AS15169", entry)
					return
				}
				if prefixes := lookup.GetPrefixesByASN(15169); len(prefixes) == 0 {
					t.Error("GetPrefixesByASN(15169) returned nothing during a swap")
					return
				}
			}
		}()
	}

	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			srv.setBody(t, fixtureTSVv2, `"v2"`)
		} else {
			srv.setBody(t, fixtureTSVv1, `"v1"`)
		}
		if _, err := lookup.UpdateDatabase(); err != nil {
			t.Errorf("UpdateDatabase() error = %v", err)
			break
		}
	}

	close(done)
	wg.Wait()
}

// waitForRequests waits until the server saw at least want requests.
func waitForRequests(t *testing.T, srv *fixtureServer, want int, timeout time.Duration) int {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		requests, _ := srv.counts()
		if requests >= want {
			return requests
		}
		if time.Now().After(deadline) {
			t.Fatalf("requests = %d after %s, want at least %d", requests, timeout, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestScheduleUpdateDatabase_FiresRepeatedly(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	if err := lookup.ScheduleUpdateDatabase(10 * time.Millisecond); err != nil {
		t.Fatalf("ScheduleUpdateDatabase() error = %v", err)
	}
	t.Cleanup(lookup.CancelScheduledUpdate)

	waitForRequests(t, srv, 4, 5*time.Second)
}

func TestScheduleUpdateDatabase_AppliesNewData(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	updated := make(chan struct{}, 1)
	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithOnUpdate(func(*ASNDatabase) {
			select {
			case updated <- struct{}{}:
			default:
			}
		}),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	if err := lookup.ScheduleUpdateDatabase(10 * time.Millisecond); err != nil {
		t.Fatalf("ScheduleUpdateDatabase() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	srv.setBody(t, fixtureTSVv2, `"v2"`)

	select {
	case <-updated:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the scheduled update to apply")
	}

	if entry := lookup.LookupASN(net.ParseIP("9.9.9.9")); entry == nil || entry.GetASN() != 19281 {
		t.Errorf("LookupASN(9.9.9.9) = %v, want AS19281", entry)
	}
}

func TestCancelScheduledUpdate_StopsRequests(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	if err := lookup.ScheduleUpdateDatabase(10 * time.Millisecond); err != nil {
		t.Fatalf("ScheduleUpdateDatabase() error = %v", err)
	}
	waitForRequests(t, srv, 3, 5*time.Second)

	lookup.CancelScheduledUpdate()
	after, _ := srv.counts()

	time.Sleep(100 * time.Millisecond)

	if requests, _ := srv.counts(); requests != after {
		t.Errorf("requests = %d after cancel, want %d", requests, after)
	}

	// Safe to call twice, and safe when nothing is scheduled.
	lookup.CancelScheduledUpdate()
	lookup.CancelScheduledUpdate()
}

func TestScheduleUpdateDatabase_InvalidInterval(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	for _, interval := range []time.Duration{0, -time.Second} {
		if err := lookup.ScheduleUpdateDatabase(interval); err == nil {
			t.Errorf("ScheduleUpdateDatabase(%s) error = nil, want an error", interval)
		}
	}

	before, _ := srv.counts()
	time.Sleep(50 * time.Millisecond)
	if requests, _ := srv.counts(); requests != before {
		t.Error("expected no worker to be started for an invalid interval")
	}
}

func TestScheduleUpdateDatabase_ReschedulingLeavesOneWorker(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	baseline := runtime.NumGoroutine()

	for i := 0; i < 3; i++ {
		if err := lookup.ScheduleUpdateDatabase(20 * time.Millisecond); err != nil {
			t.Fatalf("ScheduleUpdateDatabase() error = %v", err)
		}
	}

	if got := runtime.NumGoroutine(); got > baseline+2 {
		t.Errorf("goroutines = %d after rescheduling, baseline %d", got, baseline)
	}

	lookup.CancelScheduledUpdate()

	// Give the HTTP transport a moment to retire idle connections.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > baseline {
		t.Errorf("goroutines = %d after cancel, want <= %d", got, baseline)
	}
}

func TestScheduleUpdateDatabase_FailOpen(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	failed := make(chan struct{}, 1)
	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithOnError(func(error) {
			select {
			case failed <- struct{}{}:
			default:
			}
		}),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	before := lookup.Database()

	srv.setStatus(http.StatusInternalServerError)

	if err := lookup.ScheduleUpdateDatabase(10 * time.Millisecond); err != nil {
		t.Fatalf("ScheduleUpdateDatabase() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the update to fail")
	}

	if lookup.Database() != before {
		t.Error("expected the snapshot to survive a failed scheduled update")
	}
	if entry := lookup.LookupASN(net.ParseIP("8.8.8.8")); entry == nil {
		t.Error("lookups must keep working after a failed scheduled update")
	}
	if lookup.LastError() == nil {
		t.Error("expected LastError to be set")
	}

	// The worker stays alive and simply waits out the backoff.
	if delay := lookup.nextDelay(); delay < minRetryInterval {
		t.Errorf("nextDelay() = %s after a failure, want at least %s", delay, minRetryInterval)
	}
}

func TestNextDelay(t *testing.T) {
	lookup := &ASNLookup{interval: DefaultUpdateInterval}

	interval := float64(DefaultUpdateInterval)
	low := time.Duration(interval * (1 - updateJitterFraction))
	high := time.Duration(interval * (1 + updateJitterFraction))

	for i := 0; i < 20; i++ {
		delay := lookup.nextDelay()
		if delay < low || delay > high {
			t.Fatalf("nextDelay() = %s, want within [%s, %s]", delay, low, high)
		}
	}

	ladder := []time.Duration{
		15 * time.Minute,
		30 * time.Minute,
		time.Hour,
		2 * time.Hour,
		2 * time.Hour,
		2 * time.Hour,
	}
	for i, want := range ladder {
		lookup.failures = i + 1
		if got := lookup.nextDelay(); got != want {
			t.Errorf("nextDelay() with %d failures = %s, want %s", i+1, got, want)
		}
	}

	// A success resets the ladder.
	lookup.recordSuccess()
	if got := lookup.nextDelay(); got < low {
		t.Errorf("nextDelay() after a success = %s, want the jittered interval", got)
	}
}

func TestASNLookup_ConcurrentUpdatesAreSerialised(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile, WithURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := lookup.UpdateDatabase(); err != nil {
				t.Errorf("UpdateDatabase() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if entry := lookup.LookupASN(net.ParseIP("8.8.8.8")); entry == nil {
		t.Error("expected lookups to keep working after concurrent updates")
	}
}

func TestOptions_NoOpGuards(t *testing.T) {
	client := &http.Client{}

	l := &ASNLookup{
		url:      ASN_DATABASE_URL,
		client:   client,
		interval: DefaultUpdateInterval,
	}

	WithHTTPClient(nil)(l)
	if l.client != client {
		t.Error("WithHTTPClient(nil) replaced the configured client")
	}

	WithURL("")(l)
	if l.url != ASN_DATABASE_URL {
		t.Errorf("WithURL(\"\") changed the URL to %q", l.url)
	}

	WithUpdateInterval(0)(l)
	if l.interval != DefaultUpdateInterval {
		t.Errorf("WithUpdateInterval(0) changed the interval to %s", l.interval)
	}

	WithUpdateInterval(-time.Hour)(l)
	if l.interval != DefaultUpdateInterval {
		t.Errorf("WithUpdateInterval(negative) changed the interval to %s", l.interval)
	}
}

func TestOptions_ApplyValues(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	l := &ASNLookup{}

	opts := []Option{
		WithHTTPClient(client),
		WithURL("http://example.invalid/db.tsv.gz"),
		WithUpdateInterval(90 * time.Minute),
		WithMaxCacheAge(3 * time.Hour),
		WithAutoUpdate(true),
		WithOnUpdate(func(*ASNDatabase) {}),
		WithOnError(func(error) {}),
	}
	for _, opt := range opts {
		opt(l)
	}

	if l.client != client {
		t.Error("WithHTTPClient did not set the client")
	}
	if l.url != "http://example.invalid/db.tsv.gz" {
		t.Errorf("url = %q, want the overridden value", l.url)
	}
	if l.interval != 90*time.Minute {
		t.Errorf("interval = %s, want 1h30m", l.interval)
	}
	if l.maxCacheAge != 3*time.Hour {
		t.Errorf("maxCacheAge = %s, want 3h", l.maxCacheAge)
	}
	if !l.autoUpdate {
		t.Error("WithAutoUpdate(true) did not enable the worker")
	}
	if l.OnUpdate == nil || l.OnError == nil {
		t.Error("WithOnUpdate/WithOnError did not register the callbacks")
	}
}

func TestCacheUsable(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")

	l := &ASNLookup{cacheFile: cacheFile}
	if l.cacheUsable() {
		t.Error("cacheUsable() = true without a cache file")
	}

	writeCacheFixture(t, cacheFile, fixtureTSVv1)
	if !l.cacheUsable() {
		t.Error("cacheUsable() = false for a fresh cache file and no age limit")
	}

	l.maxCacheAge = time.Hour
	if !l.cacheUsable() {
		t.Error("cacheUsable() = false for a cache file well inside the age limit")
	}

	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cacheFile, old, old); err != nil {
		t.Fatalf("failed to age the cache file: %v", err)
	}
	if l.cacheUsable() {
		t.Error("cacheUsable() = true for a cache file past the age limit")
	}
}

func TestWithMaxCacheAge_RefreshesStaleCache(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	writeCacheFixture(t, cacheFile, fixtureTSVv1)

	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cacheFile, old, old); err != nil {
		t.Fatalf("failed to age the cache file: %v", err)
	}

	srv := newFixtureServer(t, fixtureTSVv2, `"v2"`)

	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithAutoUpdate(false),
		WithMaxCacheAge(time.Hour),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	if requests, _ := srv.counts(); requests != 1 {
		t.Errorf("requests = %d, want 1 refresh of the stale cache", requests)
	}
	if got := len(lookup.Database().IPv4Entries); got != 3 {
		t.Errorf("IPv4 entries = %d, want 3 from the refreshed database", got)
	}
}

func TestWithMaxCacheAge_KeepsFreshCache(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	writeCacheFixture(t, cacheFile, fixtureTSVv1)

	srv := newFixtureServer(t, fixtureTSVv2, `"v2"`)

	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithAutoUpdate(false),
		WithMaxCacheAge(time.Hour),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	if requests, _ := srv.counts(); requests != 0 {
		t.Errorf("requests = %d, want 0 for a cache file inside the age limit", requests)
	}
	if got := len(lookup.Database().IPv4Entries); got != 2 {
		t.Errorf("IPv4 entries = %d, want 2 from the existing cache", got)
	}
}

func TestOnUpdate_FiresOnlyWhenChanged(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	var mu sync.Mutex
	var updates []int

	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithAutoUpdate(false),
		WithOnUpdate(func(db *ASNDatabase) {
			mu.Lock()
			defer mu.Unlock()
			updates = append(updates, len(db.IPv4Entries))
		}),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	// The constructor's initial load does not go through UpdateDatabase.
	mu.Lock()
	if len(updates) != 0 {
		t.Errorf("OnUpdate fired %d times during construction, want 0", len(updates))
	}
	mu.Unlock()

	// Unchanged upstream: 304, no callback.
	if changed, err := lookup.UpdateDatabase(); err != nil || changed {
		t.Fatalf("UpdateDatabase() = (%t, %v), want (false, nil)", changed, err)
	}
	mu.Lock()
	if len(updates) != 0 {
		t.Errorf("OnUpdate fired %d times on an unchanged database, want 0", len(updates))
	}
	mu.Unlock()

	// Changed upstream: exactly one callback with the new snapshot.
	srv.setBody(t, fixtureTSVv2, `"v2"`)
	if changed, err := lookup.UpdateDatabase(); err != nil || !changed {
		t.Fatalf("UpdateDatabase() = (%t, %v), want (true, nil)", changed, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 1 {
		t.Fatalf("OnUpdate fired %d times, want 1", len(updates))
	}
	if updates[0] != 3 {
		t.Errorf("OnUpdate received %d IPv4 entries, want 3", updates[0])
	}
}

func TestOnError_FiresOnEveryFailedRefresh(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	var mu sync.Mutex
	var errs []error

	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithAutoUpdate(false),
		WithOnError(func(err error) {
			mu.Lock()
			defer mu.Unlock()
			errs = append(errs, err)
		}),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	srv.setStatus(http.StatusInternalServerError)

	for i := 0; i < 3; i++ {
		if _, err := lookup.UpdateDatabase(); err == nil {
			t.Fatalf("UpdateDatabase() #%d succeeded, want an error", i+1)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(errs) != 3 {
		t.Fatalf("OnError fired %d times, want 3", len(errs))
	}
	for i, err := range errs {
		if err == nil {
			t.Errorf("OnError call %d received a nil error", i)
		}
	}
}

func TestLastUpdateAndLastError_Transitions(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	lookup, err := NewASNLookup(cacheFile,
		WithURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithAutoUpdate(false),
	)
	if err != nil {
		t.Fatalf("NewASNLookup() error = %v", err)
	}
	defer lookup.CancelScheduledUpdate()

	firstUpdate := lookup.LastUpdate()
	if firstUpdate.IsZero() {
		t.Fatal("LastUpdate() is zero after a successful initial load")
	}
	if lookup.LastError() != nil {
		t.Errorf("LastError() = %v, want nil after a successful load", lookup.LastError())
	}

	// A failed refresh records the error but leaves LastUpdate alone.
	srv.setStatus(http.StatusInternalServerError)
	if _, err := lookup.UpdateDatabase(); err == nil {
		t.Fatal("UpdateDatabase() succeeded, want an error")
	}
	if lookup.LastError() == nil {
		t.Error("LastError() = nil after a failed refresh")
	}
	if !lookup.LastUpdate().Equal(firstUpdate) {
		t.Error("LastUpdate() moved on a failed refresh")
	}

	// A later success clears the error and advances LastUpdate.
	srv.setStatus(0)
	srv.setBody(t, fixtureTSVv2, `"v2"`)
	if _, err := lookup.UpdateDatabase(); err != nil {
		t.Fatalf("UpdateDatabase() error = %v", err)
	}
	if lookup.LastError() != nil {
		t.Errorf("LastError() = %v, want nil after a successful refresh", lookup.LastError())
	}
	if !lookup.LastUpdate().After(firstUpdate) {
		t.Error("LastUpdate() did not advance after a successful refresh")
	}
}

func TestASNLookup_NilSnapshot(t *testing.T) {
	var l ASNLookup

	if entry := l.LookupASN(net.ParseIP("8.8.8.8")); entry != nil {
		t.Errorf("LookupASN() = %v, want nil without a snapshot", entry)
	}
	if db := l.Database(); db != nil {
		t.Errorf("Database() = %v, want nil without a snapshot", db)
	}

	prefixes := l.GetPrefixesByASN(15169)
	if prefixes == nil {
		t.Fatal("GetPrefixesByASN() = nil, want an empty non-nil slice")
	}
	if len(prefixes) != 0 {
		t.Errorf("GetPrefixesByASN() = %v, want empty", prefixStrings(prefixes))
	}

	// Cancelling an unscheduled holder must be a no-op rather than a panic.
	l.CancelScheduledUpdate()
}

func TestJitter_StaysWithinBounds(t *testing.T) {
	interval := time.Hour
	lo := time.Duration(float64(interval) * (1 - updateJitterFraction))
	hi := time.Duration(float64(interval) * (1 + updateJitterFraction))

	for i := 0; i < 1000; i++ {
		got := jitter(interval)
		if got < lo || got > hi {
			t.Fatalf("jitter(%s) = %s, want within [%s, %s]", interval, got, lo, hi)
		}
	}

	// The floor keeps a tiny interval from collapsing to zero or negative.
	for i := 0; i < 1000; i++ {
		if got := jitter(time.Microsecond); got < time.Millisecond {
			t.Fatalf("jitter(1µs) = %s, want at least 1ms", got)
		}
	}
}

func TestNextDelay_ZeroIntervalFallsBackToDefault(t *testing.T) {
	l := &ASNLookup{}

	got := l.nextDelay()
	base := time.Duration(DefaultUpdateInterval)
	lo := time.Duration(float64(base) * (1 - updateJitterFraction))
	hi := time.Duration(float64(base) * (1 + updateJitterFraction))
	if got < lo || got > hi {
		t.Errorf("nextDelay() = %s, want a jittered %s", got, DefaultUpdateInterval)
	}
}
