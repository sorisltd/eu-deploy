// Package providers is the adapter framework for optional external edge
// services: DNS hosting and CDN/DDoS protection run by an EU provider.
//
// Every provider is optional. With no provider configured ("" or "none")
// the resolve functions return nil and eu-deploy behaves exactly as before:
// DNS is managed by hand and traffic goes straight to the server.
//
// Credentials never live in eudeploy.yaml. A factory reads them through a
// Lookup, which reads environment variables on the machine running eu
// (the deploy runner), e.g. EUDEPLOY_DNS_<PROVIDER>_TOKEN.
//
// No real adapter is registered yet. Each one is added in its own file once
// there is an account to test it against.
package providers

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// None is the explicit "no provider" value; an empty name means the same.
const None = "none"

// Lookup returns a credential or setting by name, empty when unset.
type Lookup func(name string) string

// EnvLookup reads credentials from the process environment.
func EnvLookup(name string) string { return strings.TrimSpace(os.Getenv(name)) }

// DNSRecord is one record eu-deploy wants to exist.
type DNSRecord struct {
	Name  string // fully qualified hostname, e.g. bustora.lt
	Type  string // "A" or "AAAA"
	Value string // origin server IP
	TTL   int    // seconds; 0 lets the provider choose
}

// DNSProvider creates or updates records at a DNS host. EnsureRecords must
// be idempotent: running it again with the same records changes nothing.
type DNSProvider interface {
	Name() string
	EnsureRecords(ctx context.Context, records []DNSRecord) error
}

// CDNSite describes a hostname served through a CDN in front of an origin.
type CDNSite struct {
	Hostname string
	OriginIP string
}

// CDNProvider puts a hostname behind a CDN/DDoS-protection network.
// EnsureSite must be idempotent.
type CDNProvider interface {
	Name() string
	EnsureSite(ctx context.Context, site CDNSite) error
}

// DNSFactory builds a DNS provider from credentials. It should fail with a
// message naming the missing variable rather than at first API call.
type DNSFactory func(lookup Lookup) (DNSProvider, error)

// CDNFactory builds a CDN provider from credentials.
type CDNFactory func(lookup Lookup) (CDNProvider, error)

var (
	mu  sync.RWMutex
	dns = map[string]DNSFactory{}
	cdn = map[string]CDNFactory{}
)

func normalize(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func isNone(name string) bool {
	n := normalize(name)
	return n == "" || n == None
}

// RegisterDNS makes a DNS provider available under name. It panics on an
// invalid or duplicate name, which is a programming error.
func RegisterDNS(name string, factory DNSFactory) {
	register(name, func() bool { _, ok := dns[normalize(name)]; return ok }, func() { dns[normalize(name)] = factory })
}

// RegisterCDN makes a CDN provider available under name.
func RegisterCDN(name string, factory CDNFactory) {
	register(name, func() bool { _, ok := cdn[normalize(name)]; return ok }, func() { cdn[normalize(name)] = factory })
}

func register(name string, exists func() bool, set func()) {
	mu.Lock()
	defer mu.Unlock()
	if isNone(name) {
		panic(fmt.Sprintf("providers: %q is reserved", name))
	}
	if exists() {
		panic(fmt.Sprintf("providers: %q registered twice", name))
	}
	set()
}

// DNSNames lists registered DNS providers, sorted.
func DNSNames() []string {
	mu.RLock()
	defer mu.RUnlock()
	return sortedKeys(dns)
}

// CDNNames lists registered CDN providers, sorted.
func CDNNames() []string {
	mu.RLock()
	defer mu.RUnlock()
	return sortedKeys(cdn)
}

func sortedKeys[T any](m map[string]T) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateName reports whether name is "", "none" or a registered provider
// of the given kind ("dns" or "cdn"). It needs no credentials.
func ValidateName(kind, name string) error {
	if isNone(name) {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	var known []string
	switch kind {
	case "dns":
		if _, ok := dns[normalize(name)]; ok {
			return nil
		}
		known = sortedKeys(dns)
	case "cdn":
		if _, ok := cdn[normalize(name)]; ok {
			return nil
		}
		known = sortedKeys(cdn)
	default:
		return fmt.Errorf("unknown provider kind %q", kind)
	}
	return unknownError(kind, name, known)
}

func unknownError(kind, name string, known []string) error {
	available := "none are available yet"
	if len(known) > 0 {
		available = "available: " + strings.Join(known, ", ")
	}
	return fmt.Errorf("edge.%s.provider %q is not supported (use %q to turn it off; %s)", kind, name, None, available)
}

// ResolveDNS returns the configured DNS provider, or nil when none is set.
func ResolveDNS(name string, lookup Lookup) (DNSProvider, error) {
	if isNone(name) {
		return nil, nil
	}
	if err := ValidateName("dns", name); err != nil {
		return nil, err
	}
	mu.RLock()
	factory := dns[normalize(name)]
	mu.RUnlock()
	provider, err := factory(lookup)
	if err != nil {
		return nil, fmt.Errorf("edge.dns.provider %q: %w", name, err)
	}
	return provider, nil
}

// ResolveCDN returns the configured CDN provider, or nil when none is set.
func ResolveCDN(name string, lookup Lookup) (CDNProvider, error) {
	if isNone(name) {
		return nil, nil
	}
	if err := ValidateName("cdn", name); err != nil {
		return nil, err
	}
	mu.RLock()
	factory := cdn[normalize(name)]
	mu.RUnlock()
	provider, err := factory(lookup)
	if err != nil {
		return nil, fmt.Errorf("edge.cdn.provider %q: %w", name, err)
	}
	return provider, nil
}

// RecordType returns "A" for IPv4 and "AAAA" for IPv6 origins, or "" when
// origin is not an IP address (a DNS name cannot be the target of an A record).
func RecordType(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}
	if strings.Contains(origin, ":") {
		return "AAAA"
	}
	for _, r := range origin {
		if (r < '0' || r > '9') && r != '.' {
			return ""
		}
	}
	return "A"
}

// Apply runs the configured providers for a deployed site. Nil providers
// are skipped, so with nothing configured it does nothing. Hostnames that
// are IP addresses are skipped: they need no DNS and cannot go behind a CDN.
func Apply(ctx context.Context, dnsProvider DNSProvider, cdnProvider CDNProvider, hostnames []string, originIP string) error {
	if dnsProvider == nil && cdnProvider == nil {
		return nil
	}
	recordType := RecordType(originIP)
	if recordType == "" {
		return fmt.Errorf("edge providers need the server's IP address as origin, got %q", originIP)
	}

	var names []string
	seen := map[string]bool{}
	for _, hostname := range hostnames {
		hostname = normalize(hostname)
		if hostname == "" || seen[hostname] || RecordType(hostname) != "" {
			continue
		}
		seen[hostname] = true
		names = append(names, hostname)
	}
	if len(names) == 0 {
		return nil
	}

	if dnsProvider != nil {
		records := make([]DNSRecord, 0, len(names))
		for _, name := range names {
			records = append(records, DNSRecord{Name: name, Type: recordType, Value: originIP})
		}
		if err := dnsProvider.EnsureRecords(ctx, records); err != nil {
			return fmt.Errorf("%s DNS: %w", dnsProvider.Name(), err)
		}
	}
	if cdnProvider != nil {
		for _, name := range names {
			if err := cdnProvider.EnsureSite(ctx, CDNSite{Hostname: name, OriginIP: originIP}); err != nil {
				return fmt.Errorf("%s CDN %s: %w", cdnProvider.Name(), name, err)
			}
		}
	}
	return nil
}
