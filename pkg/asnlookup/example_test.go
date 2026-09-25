package asnlookup_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/elnerd/asnlookup/pkg/asnlookup"
)

// exampleTSV is a miniature stand-in for the upstream ip2asn-combined.tsv
// database, so the examples below are deterministic and need no network.
const exampleTSV = "1.0.0.0\t1.0.0.255\t13335\tUS\tCLOUDFLARENET\n" +
	"8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n" +
	"2001:4860:4860::\t2001:4860:4860:ffff:ffff:ffff:ffff:ffff\t15169\tUS\tGOOGLE\n"

// exampleCacheFile writes exampleTSV as a gzipped cache file in a temporary
// directory and returns its path together with a cleanup function.
func exampleCacheFile() (string, func()) {
	dir, err := os.MkdirTemp("", "asnlookup-example-")
	if err != nil {
		log.Fatal(err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(exampleTSV)); err != nil {
		log.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		log.Fatal(err)
	}

	cacheFile := filepath.Join(dir, "asnlookup.cache")
	if err := os.WriteFile(cacheFile, buf.Bytes(), 0644); err != nil {
		log.Fatal(err)
	}

	return cacheFile, func() { os.RemoveAll(dir) }
}

// ExampleNewASNLookup shows the recommended entry point for a long-running
// process. Pass WithAutoUpdate(false) for a short-lived one, as here.
func ExampleNewASNLookup() {
	cacheFile, cleanup := exampleCacheFile()
	defer cleanup()

	lookup, err := asnlookup.NewASNLookup(cacheFile, asnlookup.WithAutoUpdate(false))
	if err != nil {
		log.Fatal(err)
	}
	defer lookup.CancelScheduledUpdate()

	entry := lookup.LookupASN(net.ParseIP("8.8.8.8"))
	fmt.Printf("AS%d %s\n", entry.GetASN(), entry.GetASDescription())

	// Output:
	// AS15169 GOOGLE
}

// ExampleASNDatabase_LookupASN resolves a single address to its announcing AS.
func ExampleASNDatabase_LookupASN() {
	cacheFile, cleanup := exampleCacheFile()
	defer cleanup()

	db, err := asnlookup.NewASNDatabaseWithCache(cacheFile)
	if err != nil {
		log.Fatal(err)
	}

	entry := db.LookupASN(net.ParseIP("1.0.0.42"))
	if entry == nil {
		fmt.Println("no match")
		return
	}

	ipNet := entry.GetIPNet()
	fmt.Printf("%s AS%d %s %s\n",
		ipNet.String(), entry.GetASN(), entry.GetCountryCode(), entry.GetASDescription())

	// An address outside every known range yields nil.
	fmt.Println(db.LookupASN(net.ParseIP("203.0.113.1")) == nil)

	// Output:
	// 1.0.0.0/24 AS13335 US CLOUDFLARENET
	// true
}

// ExampleASNDatabase_GetPrefixesByASN lists every prefix announced by an AS.
// IPv4 prefixes come first, then IPv6, both in ascending order.
func ExampleASNDatabase_GetPrefixesByASN() {
	cacheFile, cleanup := exampleCacheFile()
	defer cleanup()

	db, err := asnlookup.NewASNDatabaseWithCache(cacheFile)
	if err != nil {
		log.Fatal(err)
	}

	for _, prefix := range db.GetPrefixesByASN(15169) {
		fmt.Println(prefix)
	}

	// Output:
	// 8.8.8.0/24
	// 2001:4860:4860::/48
}
