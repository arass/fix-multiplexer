package config

import (
	"errors"
	"net/netip"
)

// IPPrefixes validates and compiles source restrictions. Nil is unrestricted;
// an explicit empty list denies every source. The result owns its storage.
func (c Client) IPPrefixes() ([]netip.Prefix, error) {
	if c.AllowedIPs == nil {
		return nil, nil
	}
	if len(c.AllowedIPs) > 128 {
		return nil, errors.New("too many allowed_ips entries")
	}
	prefixes := make([]netip.Prefix, 0, len(c.AllowedIPs))
	seen := make(map[netip.Prefix]bool)
	for _, value := range c.AllowedIPs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			addr, e := netip.ParseAddr(value)
			if e != nil || addr.Zone() != "" {
				return nil, errors.New("invalid allowed_ips address or range")
			}
			addr = addr.Unmap()
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		} else if prefix.Addr().Is4In6() {
			// Normalize mapped IPv4 prefixes consistently with socket addresses.
			if prefix.Bits() < 96 {
				return nil, errors.New("mapped IPv4 range must have at least 96 prefix bits")
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		prefix = prefix.Masked()
		if seen[prefix] {
			return nil, errors.New("duplicate allowed_ips address or range")
		}
		seen[prefix] = true
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}
