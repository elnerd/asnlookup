package asnlookup

import (
	"net/netip"
	"testing"
)

// seedAddr returns the raw bytes of a literal address for use as a fuzz seed.
// The literals below are constants, so a parse failure is a programming error.
func seedAddr(s string) []byte {
	a, err := netip.ParseAddr(s)
	if err != nil {
		panic("invalid fuzz seed address " + s + ": " + err.Error())
	}
	return addrBytes(a)
}

// FuzzRangeToPrefixes checks the invariants of the range -> CIDR conversion:
// the result is either empty (invalid input) or an ordered, contiguous and
// gap-free cover of exactly start..end made of properly masked prefixes.
func FuzzRangeToPrefixes(f *testing.F) {
	seeds := [][2]string{
		{"8.8.8.0", "8.8.8.255"},
		{"1.0.0.0", "1.0.0.200"},
		{"192.168.1.5", "192.168.1.8"},
		{"10.0.0.1", "10.0.0.1"},
		{"0.0.0.0", "255.255.255.255"},
		{"255.255.255.252", "255.255.255.255"},
		{"10.0.0.10", "10.0.0.1"},
		{"2001:4860:4860::", "2001:4860:4860:0:ffff:ffff:ffff:ffff"},
		{"2001:db8::", "2001:db8::5"},
		{"::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
	}
	for _, s := range seeds {
		f.Add(seedAddr(s[0]), seedAddr(s[1]))
	}
	// Mismatched families and degenerate lengths.
	f.Add([]byte{10, 0, 0, 1}, make([]byte, 16))
	f.Add([]byte{}, []byte{})
	f.Add([]byte{1, 2, 3}, []byte{1, 2, 3})

	f.Fuzz(func(t *testing.T, startBytes, endBytes []byte) {
		start, okStart := netip.AddrFromSlice(startBytes)
		end, okEnd := netip.AddrFromSlice(endBytes)

		prefixes := rangeToPrefixes(start, end)

		start = start.Unmap()
		end = end.Unmap()
		valid := okStart && okEnd && start.BitLen() == end.BitLen() && !end.Less(start)

		if !valid {
			if len(prefixes) != 0 {
				t.Fatalf("rangeToPrefixes(%v, %v) = %v, want empty for invalid input", start, end, prefixes)
			}
			return
		}

		if len(prefixes) == 0 {
			t.Fatalf("rangeToPrefixes(%s, %s) returned no prefixes for a valid range", start, end)
		}
		// A minimal cover never needs more than 2*bits-2 prefixes.
		if limit := 2 * start.BitLen(); len(prefixes) > limit {
			t.Fatalf("rangeToPrefixes(%s, %s) returned %d prefixes, want at most %d", start, end, len(prefixes), limit)
		}

		cur := start
		for i, p := range prefixes {
			if p.Masked() != p {
				t.Fatalf("prefix %d (%s) is not masked", i, p)
			}
			if p.Addr() != cur {
				t.Fatalf("prefix %d starts at %s, expected %s", i, p.Addr(), cur)
			}

			last := lastAddr(p.Addr(), p.Bits())
			if end.Less(last) {
				t.Fatalf("prefix %d (%s) ends at %s, past the requested end %s", i, p, last, end)
			}

			if i == len(prefixes)-1 {
				if last != end {
					t.Fatalf("last prefix (%s) ends at %s, expected %s", p, last, end)
				}
				break
			}

			next, ok := nextAddr(last)
			if !ok {
				t.Fatalf("unexpected wrap-around after prefix %d (%s)", i, p)
			}
			cur = next
		}
	})
}

// FuzzTrailingZeroBits checks that the bit count matches the address family
// and is consistent with masking the address to that length.
func FuzzTrailingZeroBits(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{255, 255, 255, 255})
	f.Add([]byte{192, 168, 1, 0})
	f.Add(make([]byte, 16))

	f.Fuzz(func(t *testing.T, b []byte) {
		a, ok := netip.AddrFromSlice(b)
		if !ok {
			return
		}

		got := trailingZeroBits(a)
		if got < 0 || got > a.BitLen() {
			t.Fatalf("trailingZeroBits(%s) = %d, out of range [0, %d]", a, got, a.BitLen())
		}

		prefixLen := a.BitLen() - got
		p := netip.PrefixFrom(a, prefixLen)
		if p.Masked() != p {
			t.Fatalf("%s/%d is not masked, so trailingZeroBits(%s) = %d is too small", a, prefixLen, a, got)
		}
	})
}
