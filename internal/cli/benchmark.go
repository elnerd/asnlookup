package cli

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"time"

	"github.com/elnerd/asnlookup/pkg/asnlookup"
)

// runTimingBenchmark exercises the holder so a scheduled refresh can swap the
// snapshot underneath the running benchmark.
func runTimingBenchmark(w io.Writer, lookup *asnlookup.ASNLookup, duration time.Duration) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	runIPToASNBenchmark(w, lookup, rng, duration)
	runASNToPrefixBenchmark(w, lookup, rng, duration)
}

// runIPToASNBenchmark measures IP -> ASN lookups using random IPv4 addresses.
func runIPToASNBenchmark(w io.Writer, lookup *asnlookup.ASNLookup, rng *rand.Rand, duration time.Duration) {
	fmt.Fprintf(w, "Running IP to ASN timing benchmark for %.0f seconds...\n", duration.Seconds())

	startTime := time.Now()
	endTime := startTime.Add(duration)
	count := 0
	hits := 0

	for time.Now().Before(endTime) {
		// Generate random IPv4 address
		randomIP := make(net.IP, 4)
		binary.BigEndian.PutUint32(randomIP, rng.Uint32())

		// Perform lookup
		if lookup.LookupASN(randomIP) != nil {
			hits++
		}
		count++
	}

	printBenchmarkResults(w, "IP to ASN", count, hits, time.Since(startTime))
}

// runASNToPrefixBenchmark measures ASN -> prefix lookups using random ASNs
// picked from the loaded database.
func runASNToPrefixBenchmark(w io.Writer, lookup *asnlookup.ASNLookup, rng *rand.Rand, duration time.Duration) {
	fmt.Fprintf(w, "\nRunning ASN to prefix timing benchmark for %.0f seconds...\n", duration.Seconds())

	asns := collectASNs(lookup.Database())
	if len(asns) == 0 {
		fmt.Fprintln(w, "  No ASNs available in the database, skipping benchmark")
		return
	}

	startTime := time.Now()
	endTime := startTime.Add(duration)
	count := 0
	prefixCount := 0

	for time.Now().Before(endTime) {
		asn := asns[rng.Intn(len(asns))]

		prefixCount += len(lookup.GetPrefixesByASN(asn))
		count++
	}

	elapsed := time.Since(startTime)
	printBenchmarkResults(w, "ASN to prefix", count, -1, elapsed)
	fmt.Fprintf(w, "  Prefixes/lookup:  %.2f\n", float64(prefixCount)/float64(count))
}

// collectASNs returns the distinct ASNs present in the database.
func collectASNs(db *asnlookup.ASNDatabase) []int {
	seen := make(map[int]struct{})
	asns := make([]int, 0)
	if db == nil {
		return asns
	}
	for _, entries := range [][]asnlookup.ASNEntry{db.IPv4Entries, db.IPv6Entries} {
		for i := range entries {
			asn := entries[i].ASN
			if _, ok := seen[asn]; ok {
				continue
			}
			seen[asn] = struct{}{}
			asns = append(asns, asn)
		}
	}
	return asns
}

func printBenchmarkResults(w io.Writer, name string, count, hits int, duration time.Duration) {
	fmt.Fprintf(w, "\n%s benchmark results:\n", name)
	fmt.Fprintf(w, "  Total lookups:    %d\n", count)
	if hits >= 0 {
		fmt.Fprintf(w, "  Successful:       %d\n", hits)
	}
	fmt.Fprintf(w, "  Duration:         %.2f seconds\n", duration.Seconds())
	fmt.Fprintf(w, "  Lookups/second:   %.2f\n", float64(count)/duration.Seconds())
}
