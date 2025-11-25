# ASN Lookup

A high-performance Go library and CLI tool for looking up Autonomous System Numbers (ASN) from IPv4 and IPv6 addresses.

## Features

- **Fast lookups**: 3+ million lookups per second using binary search
- **IPv4 and IPv6 support**: Handles both address families
- **Automatic caching**: Downloads and caches the ASN database locally
- **Multiple output formats**: CSV and JSON
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

### Custom Cache File

```go
db, err := asnlookup.NewASNDatabaseWithCache("/var/cache/asn.cache")
if err != nil {
    panic(err)
}
```

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

### Custom Cache File

```bash
$ asnlookup 8.8.8.8 --cache-file /var/cache/asn.cache
```

### Performance Benchmark

```bash
$ asnlookup --timing
Running timing benchmark for 5 seconds...

Benchmark Results:
  Total lookups:    16089822
  Duration:         5.00 seconds
  Lookups/second:   3217963.92
```

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

Creates a new ASN database with a custom cache file location.

### Methods

#### LookupASN

```go
func (db *ASNDatabase) LookupASN(ip net.IP) ASNEntryInterface
```

Looks up an IP address and returns the corresponding ASN entry. Returns `nil` if not found.

#### ASNEntry Methods

```go
func (e ASNEntry) GetASN() int
func (e ASNEntry) GetCountryCode() string
func (e ASNEntry) GetASDescription() string
func (e ASNEntry) GetIPNet() net.IPNet
```

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
