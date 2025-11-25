package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"github.com/elnerd/asnlookup/pkg/asnlookup"
)

func main() {
	// Parse command-line arguments
	cacheFile := flag.String("cache-file", asnlookup.DEFAULT_CACHE_FILE, "Path to cache file")
	timing := flag.Bool("timing", false, "Run timing benchmark for 5 seconds")
	format := flag.String("format", "csv", "Output format: csv or json")
	flag.Parse()

	// Load ASN database
	db, err := asnlookup.NewASNDatabaseWithCache(*cacheFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading ASN database: %v\n", err)
		os.Exit(1)
	}

	// Handle timing mode
	if *timing {
		runTimingBenchmark(db)
		return
	}

	// Check if IP argument is provided
	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s <ip> [--cache-file /path/to/cache] [--format csv|json] [--timing]\n", os.Args[0])
		os.Exit(1)
	}

	ipStr := args[0]

	// Parse IP address
	ip := net.ParseIP(ipStr)
	if ip == nil {
		fmt.Fprintf(os.Stderr, "Error: invalid IP address: %s\n", ipStr)
		os.Exit(1)
	}

	// Lookup ASN
	entry := db.LookupASN(ip)
	if entry == nil {
		fmt.Fprintf(os.Stderr, "No ASN found for IP: %s\n", ipStr)
		os.Exit(1)
	}

	// Print result based on format
	ipNet := entry.GetIPNet()
	switch strings.ToLower(*format) {
	case "json":
		result := map[string]interface{}{
			"net":     ipNet.String(),
			"as":      entry.GetASN(),
			"country": strings.ToLower(entry.GetCountryCode()),
			"as_desc": entry.GetASDescription(),
		}
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonBytes))
	case "csv":
		fmt.Printf("%s, AS%d, %s, \"%s\"\n",
			ipNet.String(),
			entry.GetASN(),
			entry.GetCountryCode(),
			entry.GetASDescription())
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown format '%s'. Use 'csv' or 'json'\n", *format)
		os.Exit(1)
	}
}

func runTimingBenchmark(db *asnlookup.ASNDatabase) {
	fmt.Println("Running timing benchmark for 5 seconds...")

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	startTime := time.Now()
	endTime := startTime.Add(5 * time.Second)
	count := 0

	for time.Now().Before(endTime) {
		// Generate random IPv4 address
		randomIP := make(net.IP, 4)
		binary.BigEndian.PutUint32(randomIP, rng.Uint32())

		// Perform lookup
		db.LookupASN(randomIP)
		count++
	}

	duration := time.Since(startTime)
	lookupsPerSecond := float64(count) / duration.Seconds()

	fmt.Printf("\nBenchmark Results:\n")
	fmt.Printf("  Total lookups:    %d\n", count)
	fmt.Printf("  Duration:         %.2f seconds\n", duration.Seconds())
	fmt.Printf("  Lookups/second:   %.2f\n", lookupsPerSecond)
}
