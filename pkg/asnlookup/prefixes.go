package asnlookup

import (
	"net"
	"net/netip"
)

// asnEntryRefs holds the positions of an ASN's entries in the database slices.
type asnEntryRefs struct {
	v4 []int32 // indices into ASNDatabase.IPv4Entries
	v6 []int32 // indices into ASNDatabase.IPv6Entries
}

// buildASNIndex populates db.asnIndex from IPv4Entries/IPv6Entries.
// It must be called after the entry slices have been sorted.
func (db *ASNDatabase) buildASNIndex() {
	index := make(map[int]asnEntryRefs, len(db.IPv4Entries)/4+1)

	for i := range db.IPv4Entries {
		asn := db.IPv4Entries[i].ASN
		refs := index[asn]
		refs.v4 = append(refs.v4, int32(i))
		index[asn] = refs
	}

	for i := range db.IPv6Entries {
		asn := db.IPv6Entries[i].ASN
		refs := index[asn]
		refs.v6 = append(refs.v6, int32(i))
		index[asn] = refs
	}

	db.asnIndex = index
}

// GetPrefixesByASN returns every IPv4 and IPv6 CIDR prefix announced by asn
// according to the loaded database. IPv4 prefixes come first (ascending),
// followed by IPv6 prefixes (ascending). An unknown ASN yields an empty slice.
func (db *ASNDatabase) GetPrefixesByASN(asn int) []netip.Prefix {
	prefixes := []netip.Prefix{}
	if db == nil {
		return prefixes
	}

	// Always go through indexOnce: reading db.asnIndex first would race with
	// the goroutine building it. After the first call Do is a cheap atomic load.
	db.indexOnce.Do(db.buildASNIndex)

	refs, ok := db.asnIndex[asn]
	if !ok {
		return prefixes
	}

	for _, i := range refs.v4 {
		entry := &db.IPv4Entries[i]
		prefixes = append(prefixes, entryPrefixes(entry)...)
	}
	for _, i := range refs.v6 {
		entry := &db.IPv6Entries[i]
		prefixes = append(prefixes, entryPrefixes(entry)...)
	}

	return prefixes
}

// entryPrefixes converts a single entry's inclusive range into exact prefixes.
func entryPrefixes(entry *ASNEntry) []netip.Prefix {
	return rangeToPrefixes(ipToAddr(entry.Start), ipToAddr(entry.End))
}

// ipToAddr converts a net.IP into a netip.Addr, unmapping IPv4-in-IPv6 values.
func ipToAddr(ip net.IP) netip.Addr {
	if ip4 := ip.To4(); ip4 != nil {
		a, _ := netip.AddrFromSlice(ip4)
		return a
	}
	a, _ := netip.AddrFromSlice(ip)
	return a.Unmap()
}

// addrBytes returns the raw bytes of an address (4 bytes for IPv4, 16 for IPv6).
func addrBytes(a netip.Addr) []byte {
	if a.Is4() {
		b := a.As4()
		return b[:]
	}
	b := a.As16()
	return b[:]
}

// addrFromBytes rebuilds an address from raw bytes.
func addrFromBytes(b []byte) netip.Addr {
	a, _ := netip.AddrFromSlice(b)
	return a
}

// trailingZeroBits returns the number of trailing zero bits of the address.
// An all-zero address yields the full bit length.
func trailingZeroBits(a netip.Addr) int {
	b := addrBytes(a)
	count := 0
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == 0 {
			count += 8
			continue
		}
		v := b[i]
		for v&1 == 0 {
			count++
			v >>= 1
		}
		break
	}
	return count
}

// lastAddr returns the last (broadcast) address of the prefix formed by addr/bits.
func lastAddr(a netip.Addr, prefixLen int) netip.Addr {
	b := addrBytes(a)
	out := make([]byte, len(b))
	copy(out, b)
	total := len(out) * 8
	for i := prefixLen; i < total; i++ {
		out[i/8] |= byte(0x80 >> (i % 8))
	}
	return addrFromBytes(out)
}

// nextAddr returns the successor of a. ok is false when a is the last address
// of its address space (i.e. the increment would wrap around).
func nextAddr(a netip.Addr) (netip.Addr, bool) {
	b := addrBytes(a)
	out := make([]byte, len(b))
	copy(out, b)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			return addrFromBytes(out), true
		}
	}
	return netip.Addr{}, false
}

// rangeToPrefixes converts an inclusive start..end address range into the
// minimal set of exact CIDR prefixes covering it. It returns an empty slice
// for invalid input (unset addresses, mismatched families or end < start).
func rangeToPrefixes(start, end netip.Addr) []netip.Prefix {
	prefixes := []netip.Prefix{}

	if !start.IsValid() || !end.IsValid() {
		return prefixes
	}
	start = start.Unmap()
	end = end.Unmap()
	if start.BitLen() != end.BitLen() {
		return prefixes
	}
	if end.Less(start) {
		return prefixes
	}

	bits := start.BitLen()
	cur := start
	for {
		prefixLen := bits - trailingZeroBits(cur)
		last := lastAddr(cur, prefixLen)
		for prefixLen < bits && end.Less(last) {
			prefixLen++
			last = lastAddr(cur, prefixLen)
		}

		prefixes = append(prefixes, netip.PrefixFrom(cur, prefixLen))

		if !last.Less(end) {
			break
		}
		next, ok := nextAddr(last)
		if !ok {
			break
		}
		cur = next
	}

	return prefixes
}
