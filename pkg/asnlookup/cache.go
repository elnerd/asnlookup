package asnlookup

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

const ASN_DATABASE_URL = "https://iptoasn.com/data/ip2asn-combined.tsv.gz"
const DEFAULT_CACHE_FILE = "/tmp/asnlookup.cache"

type ASNEntry struct {
	Start         net.IP
	End           net.IP
	ASN           int
	CountryCode   string
	ASDescription string
}

func (e ASNEntry) GetASN() int {
	return e.ASN
}

func (e ASNEntry) GetIPNet() net.IPNet {
	// Calculate prefix length from start and end range
	prefixLen := calculatePrefixLength(e.Start, e.End)

	bits := len(e.Start) * 8
	mask := net.CIDRMask(prefixLen, bits)
	return net.IPNet{
		IP:   e.Start,
		Mask: mask,
	}
}

func (e ASNEntry) GetCountryCode() string {
	return e.CountryCode
}

func (e ASNEntry) GetASDescription() string {
	return e.ASDescription
}

// calculatePrefixLength calculates the CIDR prefix length from start and end IPs
func calculatePrefixLength(start, end net.IP) int {
	bits := len(start) * 8

	// Convert to big.Int for arithmetic
	startInt := new(big.Int).SetBytes(start)
	endInt := new(big.Int).SetBytes(end)

	// Calculate range size
	rangeSize := new(big.Int).Sub(endInt, startInt)
	rangeSize.Add(rangeSize, big.NewInt(1))

	// Find the number of bits needed
	if rangeSize.Cmp(big.NewInt(1)) == 0 {
		return bits // Single IP
	}

	// Calculate prefix length by finding the highest bit set in range-1
	rangeSizeMinus1 := new(big.Int).Sub(rangeSize, big.NewInt(1))
	hostBits := rangeSizeMinus1.BitLen()

	return bits - hostBits
}

type ASNEntryInterface interface {
	GetASN() int
	GetIPNet() net.IPNet
	GetCountryCode() string
	GetASDescription() string
}

type ASNDatabase struct {
	IPv4Entries []ASNEntry // Sorted slice for binary search (IPv4)
	IPv6Entries []ASNEntry // Sorted slice for binary search (IPv6)
}

type ASNDatabaseInterface interface {
	LookupASN(ip net.IP) ASNEntryInterface
}

func (db *ASNDatabase) LookupASN(ip net.IP) ASNEntryInterface {
	// Determine if IPv4 or IPv6
	var entries []ASNEntry
	var searchIP net.IP

	if ip4 := ip.To4(); ip4 != nil {
		entries = db.IPv4Entries
		searchIP = ip4
	} else if ip6 := ip.To16(); ip6 != nil {
		entries = db.IPv6Entries
		searchIP = ip6
	} else {
		return nil
	}

	// Binary search to find the entry where Start <= searchIP
	idx := sort.Search(len(entries), func(i int) bool {
		return bytes.Compare(entries[i].Start, searchIP) > 0
	})

	// idx points to the first entry where Start > searchIP
	// So we need to check idx-1
	if idx > 0 {
		idx--
		entry := &entries[idx]
		if bytes.Compare(searchIP, entry.Start) >= 0 && bytes.Compare(searchIP, entry.End) <= 0 {
			return entry
		}
	}

	return nil
}

func NewASNDatabase() (*ASNDatabase, error) {
	return NewASNDatabaseWithCache(DEFAULT_CACHE_FILE)
}

func NewASNDatabaseWithCache(cacheFile string) (*ASNDatabase, error) {
	// Check if we have a cache file
	if _, err := os.Stat(cacheFile); err == nil {
		return loadFromCache(cacheFile)
	}

	// Download new database file
	resp, err := http.Get(ASN_DATABASE_URL)
	if err != nil {
		return nil, fmt.Errorf("failed to download ASN database: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download ASN database: status %d", resp.StatusCode)
	}

	// Save to cache file
	out, err := os.Create(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache file: %w", err)
	}
	defer out.Close()

	// Copy downloaded content to cache
	if _, err := io.Copy(out, resp.Body); err != nil {
		return nil, fmt.Errorf("failed to save cache file: %w", err)
	}

	// Load from the newly created cache
	return loadFromCache(cacheFile)
}

func loadFromCache(filename string) (*ASNDatabase, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open cache file: %w", err)
	}
	defer file.Close()

	// Decompress gzip
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress cache file: %w", err)
	}
	defer gzReader.Close()

	db := &ASNDatabase{
		IPv4Entries: make([]ASNEntry, 0, 100000),
		IPv6Entries: make([]ASNEntry, 0, 50000),
	}

	scanner := bufio.NewScanner(gzReader)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue
		}

		startIP := net.ParseIP(fields[0])
		if startIP == nil {
			continue
		}

		endIP := net.ParseIP(fields[1])
		if endIP == nil {
			continue
		}

		asn, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}

		// Determine if IPv4 or IPv6 and normalize
		isIPv4 := startIP.To4() != nil && endIP.To4() != nil

		entry := ASNEntry{
			Start:         startIP,
			End:           endIP,
			ASN:           asn,
			CountryCode:   fields[3],
			ASDescription: fields[4],
		}

		if isIPv4 {
			// Normalize to 4-byte representation for consistent comparison
			entry.Start = startIP.To4()
			entry.End = endIP.To4()
			db.IPv4Entries = append(db.IPv4Entries, entry)
		} else {
			// Normalize to 16-byte representation
			entry.Start = startIP.To16()
			entry.End = endIP.To16()
			db.IPv6Entries = append(db.IPv6Entries, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	// Sort entries by Start IP for binary search
	sort.Slice(db.IPv4Entries, func(i, j int) bool {
		return bytes.Compare(db.IPv4Entries[i].Start, db.IPv4Entries[j].Start) < 0
	})

	sort.Slice(db.IPv6Entries, func(i, j int) bool {
		return bytes.Compare(db.IPv6Entries[i].Start, db.IPv6Entries[j].Start) < 0
	})

	return db, nil
}
