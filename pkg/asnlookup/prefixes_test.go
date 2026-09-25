package asnlookup

import (
	"net"
	"net/netip"
	"sync"
	"testing"
)

func TestRangeToPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		start    string
		end      string
		expected []string
	}{
		{
			name:     "aligned IPv4 /24",
			start:    "8.8.8.0",
			end:      "8.8.8.255",
			expected: []string{"8.8.8.0/24"},
		},
		{
			name:  "non-aligned IPv4 range",
			start: "1.0.0.0",
			end:   "1.0.0.200",
			expected: []string{
				"1.0.0.0/25",
				"1.0.0.128/26",
				"1.0.0.192/29",
				"1.0.0.200/32",
			},
		},
		{
			name:  "non-aligned start",
			start: "192.168.1.5",
			end:   "192.168.1.8",
			expected: []string{
				"192.168.1.5/32",
				"192.168.1.6/31",
				"192.168.1.8/32",
			},
		},
		{
			name:     "single IPv4 address",
			start:    "10.0.0.1",
			end:      "10.0.0.1",
			expected: []string{"10.0.0.1/32"},
		},
		{
			name:     "full IPv4 space",
			start:    "0.0.0.0",
			end:      "255.255.255.255",
			expected: []string{"0.0.0.0/0"},
		},
		{
			name:     "IPv4 range ending at top of space",
			start:    "255.255.255.252",
			end:      "255.255.255.255",
			expected: []string{"255.255.255.252/30"},
		},
		{
			name:     "aligned IPv6 /64",
			start:    "2001:4860:4860::",
			end:      "2001:4860:4860:0:ffff:ffff:ffff:ffff",
			expected: []string{"2001:4860:4860::/64"},
		},
		{
			name:     "single IPv6 address",
			start:    "2001:db8::1",
			end:      "2001:db8::1",
			expected: []string{"2001:db8::1/128"},
		},
		{
			name:  "non-aligned IPv6 range",
			start: "2001:db8::",
			end:   "2001:db8::5",
			expected: []string{
				"2001:db8::/126",
				"2001:db8::4/127",
			},
		},
		{
			name:     "full IPv6 space",
			start:    "::",
			end:      "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
			expected: []string{"::/0"},
		},
		{
			name:     "IPv6 range ending at top of space",
			start:    "ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe",
			end:      "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
			expected: []string{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe/127"},
		},
		{
			name:     "end before start",
			start:    "10.0.0.10",
			end:      "10.0.0.1",
			expected: []string{},
		},
		{
			name:     "mismatched families",
			start:    "10.0.0.1",
			end:      "2001:db8::1",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rangeToPrefixes(mustAddr(t, tt.start), mustAddr(t, tt.end))
			if len(got) != len(tt.expected) {
				t.Fatalf("got %v, expected %v", got, tt.expected)
			}
			for i, p := range got {
				if p.String() != tt.expected[i] {
					t.Errorf("prefix %d: got %s, expected %s", i, p, tt.expected[i])
				}
				if p.Masked() != p {
					t.Errorf("prefix %d (%s) is not masked", i, p)
				}
			}
		})
	}
}

func TestRangeToPrefixesInvalidAddr(t *testing.T) {
	if got := rangeToPrefixes(netip.Addr{}, netip.Addr{}); len(got) != 0 {
		t.Errorf("expected empty result for invalid addresses, got %v", got)
	}
}

func TestRangeToPrefixesCoversExactly(t *testing.T) {
	start := mustAddr(t, "1.0.0.3")
	end := mustAddr(t, "1.0.1.77")

	prefixes := rangeToPrefixes(start, end)
	if len(prefixes) == 0 {
		t.Fatal("expected at least one prefix")
	}

	if prefixes[0].Addr() != start {
		t.Errorf("first prefix starts at %s, expected %s", prefixes[0].Addr(), start)
	}

	cur := start
	for i, p := range prefixes {
		if p.Addr() != cur {
			t.Fatalf("prefix %d starts at %s, expected %s", i, p.Addr(), cur)
		}
		last := lastAddr(p.Addr(), p.Bits())
		if i == len(prefixes)-1 {
			if last != end {
				t.Fatalf("last prefix ends at %s, expected %s", last, end)
			}
			break
		}
		next, ok := nextAddr(last)
		if !ok {
			t.Fatalf("unexpected overflow after prefix %d", i)
		}
		cur = next
	}
}

func TestGetPrefixesByASN_IPv4(t *testing.T) {
	testData := "8.8.8.0\t8.8.8.255\t15169\tus\tGOOGLE\n" +
		"1.0.0.0\t1.0.0.200\t15169\tus\tGOOGLE\n" +
		"10.0.0.0\t10.0.0.255\t64512\tno\tOTHER\n"

	db, err := loadFromCache(writeTestCache(t, "v4.cache", testData))
	if err != nil {
		t.Fatalf("loadFromCache failed: %v", err)
	}

	assertPrefixes(t, db.GetPrefixesByASN(15169), []string{
		"1.0.0.0/25",
		"1.0.0.128/26",
		"1.0.0.192/29",
		"1.0.0.200/32",
		"8.8.8.0/24",
	})

	assertPrefixes(t, db.GetPrefixesByASN(64512), []string{"10.0.0.0/24"})
}

func TestGetPrefixesByASN_IPv6(t *testing.T) {
	testData := "2001:4860:4860::\t2001:4860:4860:0:ffff:ffff:ffff:ffff\t15169\tus\tGOOGLE\n" +
		"2001:db8::\t2001:db8::5\t15169\tus\tGOOGLE\n"

	db, err := loadFromCache(writeTestCache(t, "v6.cache", testData))
	if err != nil {
		t.Fatalf("loadFromCache failed: %v", err)
	}

	assertPrefixes(t, db.GetPrefixesByASN(15169), []string{
		"2001:db8::/126",
		"2001:db8::4/127",
		"2001:4860:4860::/64",
	})
}

func TestGetPrefixesByASN_Mixed(t *testing.T) {
	testData := "2001:4860::\t2001:4860:ffff:ffff:ffff:ffff:ffff:ffff\t15169\tus\tGOOGLE\n" +
		"8.8.4.0\t8.8.4.255\t15169\tus\tGOOGLE\n" +
		"8.8.8.0\t8.8.8.255\t15169\tus\tGOOGLE\n" +
		"2001:db8::\t2001:db8::ffff\t64512\tno\tOTHER\n"

	db, err := loadFromCache(writeTestCache(t, "mixed.cache", testData))
	if err != nil {
		t.Fatalf("loadFromCache failed: %v", err)
	}

	assertPrefixes(t, db.GetPrefixesByASN(15169), []string{
		"8.8.4.0/24",
		"8.8.8.0/24",
		"2001:4860::/32",
	})
}

func TestGetPrefixesByASN_NotFound(t *testing.T) {
	testData := "8.8.8.0\t8.8.8.255\t15169\tus\tGOOGLE\n"

	db, err := loadFromCache(writeTestCache(t, "notfound.cache", testData))
	if err != nil {
		t.Fatalf("loadFromCache failed: %v", err)
	}

	for _, asn := range []int{1, 0, -1} {
		got := db.GetPrefixesByASN(asn)
		if len(got) != 0 {
			t.Errorf("ASN %d: expected empty result, got %v", asn, prefixStrings(got))
		}
	}
}

func TestGetPrefixesByASN_Deterministic(t *testing.T) {
	testData := "8.8.8.0\t8.8.8.255\t15169\tus\tGOOGLE\n" +
		"1.0.0.0\t1.0.0.200\t15169\tus\tGOOGLE\n" +
		"2001:4860::\t2001:4860:ffff:ffff:ffff:ffff:ffff:ffff\t15169\tus\tGOOGLE\n"

	db, err := loadFromCache(writeTestCache(t, "deterministic.cache", testData))
	if err != nil {
		t.Fatalf("loadFromCache failed: %v", err)
	}

	first := prefixStrings(db.GetPrefixesByASN(15169))
	second := prefixStrings(db.GetPrefixesByASN(15169))
	assertPrefixes(t, db.GetPrefixesByASN(15169), first)

	if len(first) != len(second) {
		t.Fatalf("results differ in length: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("prefix %d differs between calls: %s vs %s", i, first[i], second[i])
		}
	}
}

func TestGetPrefixesByASN_MalformedEntrySkipped(t *testing.T) {
	testData := "8.8.8.255\t8.8.8.0\t15169\tus\tGOOGLE\n" +
		"9.9.9.0\t9.9.9.255\t15169\tus\tGOOGLE\n"

	db, err := loadFromCache(writeTestCache(t, "malformed.cache", testData))
	if err != nil {
		t.Fatalf("loadFromCache failed: %v", err)
	}

	assertPrefixes(t, db.GetPrefixesByASN(15169), []string{"9.9.9.0/24"})
}

func TestGetPrefixesByASN_LazyIndex(t *testing.T) {
	db := &ASNDatabase{
		IPv4Entries: []ASNEntry{
			{
				Start:         net.ParseIP("8.8.8.0").To4(),
				End:           net.ParseIP("8.8.8.255").To4(),
				ASN:           15169,
				CountryCode:   "us",
				ASDescription: "GOOGLE",
			},
		},
		IPv6Entries: []ASNEntry{
			{
				Start:         net.ParseIP("2001:4860::").To16(),
				End:           net.ParseIP("2001:4860:ffff:ffff:ffff:ffff:ffff:ffff").To16(),
				ASN:           15169,
				CountryCode:   "us",
				ASDescription: "GOOGLE",
			},
		},
	}

	if db.asnIndex != nil {
		t.Fatal("expected nil index on a hand-built database")
	}

	assertPrefixes(t, db.GetPrefixesByASN(15169), []string{
		"8.8.8.0/24",
		"2001:4860::/32",
	})

	if db.asnIndex == nil {
		t.Error("expected the index to be built lazily")
	}
}

func TestGetPrefixesByASN_NilDatabase(t *testing.T) {
	var db *ASNDatabase

	got := db.GetPrefixesByASN(15169)
	if got == nil {
		t.Fatal("GetPrefixesByASN() = nil, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("GetPrefixesByASN() = %v, want empty", prefixStrings(got))
	}
}

func TestGetPrefixesByASN_ConcurrentLazyIndex(t *testing.T) {
	db := &ASNDatabase{
		IPv4Entries: []ASNEntry{
			{
				Start:         net.ParseIP("8.8.8.0").To4(),
				End:           net.ParseIP("8.8.8.255").To4(),
				ASN:           15169,
				CountryCode:   "us",
				ASDescription: "GOOGLE",
			},
			{
				Start:         net.ParseIP("9.9.9.0").To4(),
				End:           net.ParseIP("9.9.9.255").To4(),
				ASN:           19281,
				CountryCode:   "us",
				ASDescription: "QUAD9",
			},
		},
		IPv6Entries: []ASNEntry{
			{
				Start:         net.ParseIP("2001:4860::").To16(),
				End:           net.ParseIP("2001:4860:ffff:ffff:ffff:ffff:ffff:ffff").To16(),
				ASN:           15169,
				CountryCode:   "us",
				ASDescription: "GOOGLE",
			},
		},
	}

	const goroutines = 32
	var wg sync.WaitGroup
	results := make([][]string, goroutines)

	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = prefixStrings(db.GetPrefixesByASN(15169))
		}(i)
	}
	close(start)
	wg.Wait()

	want := []string{"8.8.8.0/24", "2001:4860::/32"}
	for i, got := range results {
		if len(got) != len(want) {
			t.Fatalf("goroutine %d: got %v, want %v", i, got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Errorf("goroutine %d prefix %d: got %s, want %s", i, j, got[j], want[j])
			}
		}
	}
}

func TestEntryPrefixes_MismatchedFamilies(t *testing.T) {
	// loadFromCache never produces such an entry, but a hand-built one must
	// not panic: the families differ, so no prefix can be derived.
	entry := &ASNEntry{
		Start: net.ParseIP("10.0.0.0").To4(),
		End:   net.ParseIP("2001:db8::1").To16(),
		ASN:   64512,
	}

	if got := entryPrefixes(entry); len(got) != 0 {
		t.Errorf("entryPrefixes() = %v, want empty for mismatched families", prefixStrings(got))
	}
}

func TestIpToAddr(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want string
	}{
		{name: "4-byte IPv4", ip: net.ParseIP("1.2.3.4").To4(), want: "1.2.3.4"},
		{name: "IPv4-in-IPv6", ip: net.ParseIP("1.2.3.4").To16(), want: "1.2.3.4"},
		{name: "IPv6", ip: net.ParseIP("2001:db8::1"), want: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ipToAddr(tt.ip)
			if got.String() != tt.want {
				t.Errorf("ipToAddr() = %s, want %s", got, tt.want)
			}
		})
	}

	if got := ipToAddr(net.IP{1, 2, 3}); got.IsValid() {
		t.Errorf("ipToAddr(malformed) = %s, want an invalid address", got)
	}
}

func TestTrailingZeroBits(t *testing.T) {
	tests := []struct {
		addr string
		want int
	}{
		{addr: "0.0.0.0", want: 32},
		{addr: "255.255.255.255", want: 0},
		{addr: "192.168.1.0", want: 8},
		{addr: "10.0.0.128", want: 7},
		{addr: "::", want: 128},
		{addr: "2001:db8::", want: 99},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := trailingZeroBits(mustAddr(t, tt.addr)); got != tt.want {
				t.Errorf("trailingZeroBits(%s) = %d, want %d", tt.addr, got, tt.want)
			}
		})
	}
}

func TestNextAddr_WrapsAtTopOfSpace(t *testing.T) {
	if _, ok := nextAddr(mustAddr(t, "255.255.255.255")); ok {
		t.Error("nextAddr(255.255.255.255) reported success, want a wrap-around")
	}
	if _, ok := nextAddr(mustAddr(t, "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")); ok {
		t.Error("nextAddr(all-ones IPv6) reported success, want a wrap-around")
	}

	got, ok := nextAddr(mustAddr(t, "1.0.0.255"))
	if !ok {
		t.Fatal("nextAddr(1.0.0.255) failed unexpectedly")
	}
	if got.String() != "1.0.1.0" {
		t.Errorf("nextAddr(1.0.0.255) = %s, want 1.0.1.0", got)
	}
}
