# ASN Lookup

A high-performance Go library and CLI tool for looking up Autonomous System Numbers (ASN) from IPv4 and IPv6 addresses.

## Features

- **Fast lookups**: 3+ million lookups per second using binary search
- **IPv4 and IPv6 support**: Handles both address families
- **Automatic caching**: Downloads and caches the ASN database locally
- **Automatic refresh**: Long-running processes get an ETag-conditional freshness check every 8 hours, applied without interrupting lookups
- **Multiple output formats**: CSV and JSON
- **Reverse ASN lookup**: Find every IPv4 and IPv6 prefix announced by an ASN
- **Simple API**: Easy to integrate into your Go projects

## Installation

```bash
go get github.com/erlend/asnlookup
```

## Usage as a Library

### Basic Example

```go
package main

import (
    "fmt"
    "net"

    "github.com/elnerd/asnlookup/pkg/asnlookup"
)

func main() {
    // Load the ASN database (downloads if not cached)
    db, err := asnlookup.NewASNDatabase()
    if err != nil {
        panic(err)
    }

    // Lookup an IPv4 address
    ip := net.ParseIP("8.8.8.8")
    entry := db.LookupASN(ip)

    if entry != nil {
        fmt.Printf("ASN: %d\n", entry.GetASN())
        fmt.Printf("Country: %s\n", entry.GetCountryCode())
        fmt.Printf("Description: %s\n", entry.GetASDescription())
        fmt.Printf("Network: %s\n", entry.GetIPNet().String())
    }
}
```

### Reverse ASN Lookup

Use `GetPrefixesByASN` to retrieve every exact IPv4 and IPv6 prefix announced
by an ASN. IPv4 prefixes are returned first, followed by IPv6 prefixes; an
unknown ASN returns an empty slice.

```go
package main

import (
    "fmt"

    "github.com/elnerd/asnlookup/pkg/asnlookup"
)

func main() {
    db, err := asnlookup.NewASNDatabase()
    if err != nil {
        panic(err)
    }

    for _, prefix := range db.GetPrefixesByASN(15169) {
        fmt.Println(prefix)
    }
}
```

### Custom Cache File

```go
db, err := asnlookup.NewASNDatabaseWithCache("/var/cache/asn.cache")
if err != nil {
    panic(err)
}
```

### Keeping the Database Fresh

`NewASNDatabase` / `NewASNDatabaseWithCache` return a static snapshot: whatever
is in the cache file is what you keep, for the lifetime of the process. For a
long-running service use `NewASNLookup`, which owns the snapshot and refreshes
it in the background.

```go
lookup, err := asnlookup.NewASNLookup(asnlookup.DEFAULT_CACHE_FILE)
if err != nil {
    panic(err)
}
defer lookup.CancelScheduledUpdate()

entry := lookup.LookupASN(net.ParseIP("8.8.8.8"))
```

#### Default refresh policy

- A background worker is started automatically and checks for a new database
  every `DefaultUpdateInterval` (**8 hours**), with a random **±10 % jitter** so
  many instances do not hit the endpoint in lockstep.
- Every check is a conditional `If-None-Match` / `If-Modified-Since` request.
  When the upstream file is unchanged the server answers `304` and the check
  costs one small response — nothing is downloaded, re-parsed or swapped.
- The refresh **fails open**. A network error, a non-2xx response, a corrupt
  download or an unwritable cache never breaks lookups and never stops the
  schedule: the current snapshot keeps serving and the worker retries on a soft
  capped backoff of 15 min → 30 min → 1 h → 2 h, returning to the normal cadence
  after the first success.
- If the initial download fails but a cache file exists, the stale cache is used
  and the worker retries in the background. An error is returned only when there
  is no usable data at all.

#### Tuning or disabling it

```go
// A different cadence
lookup, err := asnlookup.NewASNLookup(path, asnlookup.WithUpdateInterval(time.Hour))

// No background worker at all (short-lived processes)
lookup, err := asnlookup.NewASNLookup(path, asnlookup.WithAutoUpdate(false))

// Change the schedule later, or stop it
err = lookup.ScheduleUpdateDatabase(2 * time.Hour)
lookup.CancelScheduledUpdate()
```

Other options: `WithHTTPClient`, `WithURL`, `WithMaxCacheAge`, `WithOnUpdate`
and `WithOnError`. Failures are also observable by polling `LastUpdate()` and
`LastError()`.

`CancelScheduledUpdate()` is the shutdown hook: it stops the worker and waits
for it to exit, so there is no goroutine leak. It is safe to call when nothing
is scheduled and safe to call twice. The legacy constructors never start a
goroutine.

#### Snapshot semantics

The database is immutable. A refresh parses a **new** `*ASNDatabase` and swaps a
pointer, so a lookup on any goroutine always observes either the complete old
database or the complete new one — never a half-loaded one — and readers are
never blocked (one atomic load per lookup).

Two consequences:

- An `ASNEntry` returned by `LookupASN` points into the snapshot it came from
  and stays valid after a swap; it simply keeps showing that snapshot's data.
- If you read the exported `IPv4Entries` / `IPv6Entries` fields, call
  `lookup.Database()` per operation instead of caching the result, otherwise you
  keep reading a superseded snapshot.

During a refresh the old and the new snapshot exist at the same time, so peak
memory is roughly double until the last in-flight lookup releases the old one.

### Refreshing Manually

```go
// Conditional download + snapshot swap; changed is false on a 304
changed, err := lookup.UpdateDatabase()

// Or work with the cache file directly
changed, err := asnlookup.DownloadASNDatabase("/var/cache/asn.cache")
db, err := asnlookup.UpdateASNDatabase("/var/cache/asn.cache")
```

The cache file is replaced atomically (written to a temp file in the same
directory, then renamed), so a failed or interrupted download never leaves a
truncated cache behind. The upstream `ETag` / `Last-Modified` values are stored
in a small `<cacheFile>.meta` sidecar.

### IPv6 Example

```go
// Lookup an IPv6 address
ip := net.ParseIP("2001:4860:4860::8888")
entry := db.LookupASN(ip)

if entry != nil {
    fmt.Printf("ASN: AS%d\n", entry.GetASN())
    fmt.Printf("Network: %s\n", entry.GetIPNet().String())
}
```

### Batch Lookups

```go
ips := []string{
    "8.8.8.8",
    "1.1.1.1",
    "2001:4860:4860::8888",
}

for _, ipStr := range ips {
    ip := net.ParseIP(ipStr)
    if entry := db.LookupASN(ip); entry != nil {
        fmt.Printf("%s -> AS%d (%s)\n",
            ipStr,
            entry.GetASN(),
            entry.GetASDescription())
    }
}
```

## Usage as a CLI Tool

### Installation

```bash
go install github.com/erlend/asnlookup/cmd@latest
```

### Basic Lookup

```bash
# IPv4 lookup (CSV format by default)
$ asnlookup 8.8.8.8
8.8.8.0/24, AS15169, US, "GOOGLE"

# IPv6 lookup
$ asnlookup 2001:4860:4860::8888
2001:4860:4860::/48, AS15169, US, "GOOGLE"
```

### JSON Output

```bash
$ asnlookup 8.8.8.8 --format json
{"as":15169,"as_desc":"GOOGLE","country":"us","net":"8.8.8.0/24"}
```

### ASN Prefix Lookup

Use `--asn` to list every IPv4 and IPv6 prefix announced by an ASN. Prefixes
are printed in CSV by default, with IPv4 prefixes first and IPv6 prefixes
after them:

```bash
$ asnlookup --asn 15169
8.8.8.0/24, AS15169, US, "GOOGLE"
2001:4860:4860::/48, AS15169, US, "GOOGLE"
```

Use `--format json` to receive one JSON object containing the ASN metadata and
the complete prefix list:

```bash
$ asnlookup --asn 15169 --format json
{"as":15169,"as_desc":"GOOGLE","country":"us","prefixes":["8.8.8.0/24","2001:4860:4860::/48"]}
```

The `--cache-file` option can be used with `--asn` in the same way as with an
IP lookup. An unknown ASN prints an error to stderr and exits unsuccessfully.

### Custom Cache File

```bash
$ asnlookup 8.8.8.8 --cache-file /var/cache/asn.cache
```

### Refreshing the Cache

The CLI exits immediately, so it does **not** start the background refresh
worker. Use `--force-update` to check for a newer database before the lookup;
an unchanged database costs a single conditional request:

```bash
$ asnlookup 8.8.8.8 --force-update
ASN database already up to date
8.8.8.0/24, AS15169, US, "GOOGLE"
```

For a long-running `--timing` run, `--update-interval` enables the background
refresher for the duration of the process:

```bash
$ asnlookup --timing --update-interval 8h
```

### Performance Benchmark

```bash
$ asnlookup --timing
Running IP to ASN timing benchmark for 5 seconds...

IP to ASN benchmark results:
  Total lookups:    5210853
  Successful:       4540582
  Duration:         5.00 seconds
  Lookups/second:   1042170.46

Running ASN to prefix timing benchmark for 5 seconds...

ASN to prefix benchmark results:
  Total lookups:    348925
  Duration:         5.23 seconds
  Lookups/second:   66772.04
  Prefixes/lookup:  20.80
```

The benchmark runs two independent 5 second phases: random IPv4 addresses are
resolved to ASNs, then random ASNs from the database are expanded to their
announced prefixes.

## API Reference

### Types

#### ASNDatabase

The main database structure containing IPv4 and IPv6 ASN entries.

```go
type ASNDatabase struct {
    IPv4Entries []ASNEntry
    IPv6Entries []ASNEntry
}
```

#### ASNEntry

Represents a single ASN entry with IP range information.

```go
type ASNEntry struct {
    Start         net.IP
    End           net.IP
    ASN           int
    CountryCode   string
    ASDescription string
}
```

### Functions

#### NewASNDatabase

```go
func NewASNDatabase() (*ASNDatabase, error)
```

Creates a new ASN database using the default cache file location (`/tmp/asnlookup.cache`). Downloads the database from iptoasn.com if not cached.

#### NewASNDatabaseWithCache

```go
func NewASNDatabaseWithCache(cacheFile string) (*ASNDatabase, error)
```

Creates a new ASN database with a custom cache file location. An existing cache
file is used as is, however stale it may be, and no background goroutine is
started.

#### NewASNLookup

```go
func NewASNLookup(cacheFile string, opts ...Option) (*ASNLookup, error)
```

Creates a refreshable holder around the database, with the automatic 8 hour
freshness check enabled by default. See
[Keeping the Database Fresh](#keeping-the-database-fresh).

#### DownloadASNDatabase / UpdateASNDatabase

```go
func DownloadASNDatabase(cacheFile string) (changed bool, err error)
func UpdateASNDatabase(cacheFile string) (*ASNDatabase, error)
```

`DownloadASNDatabase` performs a conditional download and reports whether the
cache file actually changed. `UpdateASNDatabase` does the same and returns the
freshly parsed database.

### Methods

#### LookupASN

```go
func (db *ASNDatabase) LookupASN(ip net.IP) ASNEntryInterface
```

Looks up an IP address and returns the corresponding ASN entry. Returns `nil` if not found.

#### GetPrefixesByASN

```go
func (db *ASNDatabase) GetPrefixesByASN(asn int) []netip.Prefix
```

Returns all exact CIDR prefixes announced by the given ASN according to the
cached database. IPv4 prefixes are returned first in ascending order, followed
by IPv6 prefixes in ascending order. The returned prefixes are masked and
preserve the exact address ranges in the database. An unknown, zero, or
negative ASN returns an empty slice.

#### ASNEntry Methods

```go
func (e ASNEntry) GetASN() int
func (e ASNEntry) GetCountryCode() string
func (e ASNEntry) GetASDescription() string
func (e ASNEntry) GetIPNet() net.IPNet
```

#### ASNLookup Methods

```go
func (l *ASNLookup) LookupASN(ip net.IP) ASNEntryInterface
func (l *ASNLookup) GetPrefixesByASN(asn int) []netip.Prefix
func (l *ASNLookup) Database() *ASNDatabase
func (l *ASNLookup) UpdateDatabase() (changed bool, err error)
func (l *ASNLookup) ScheduleUpdateDatabase(interval time.Duration) error
func (l *ASNLookup) CancelScheduledUpdate()
func (l *ASNLookup) LastUpdate() time.Time
func (l *ASNLookup) LastError() error
```

`ScheduleUpdateDatabase` rejects a non-positive interval and cancels any
previous schedule first, so at most one worker is ever running.

## Data Source

This library uses the ASN database from [iptoasn.com](https://iptoasn.com/), which provides:
- IPv4 and IPv6 ASN mappings
- Country codes
- AS descriptions

The database is downloaded and cached locally on first use.

## Testing

Run the test suite:

```bash
go test ./pkg/asnlookup/
```

## License

[Add your license here]

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
