package providers

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeDNS struct {
	calls [][]DNSRecord
	err   error
}

func (f *fakeDNS) Name() string { return "fake-dns" }
func (f *fakeDNS) EnsureRecords(_ context.Context, records []DNSRecord) error {
	f.calls = append(f.calls, records)
	return f.err
}

type fakeCDN struct {
	sites []CDNSite
}

func (f *fakeCDN) Name() string { return "fake-cdn" }
func (f *fakeCDN) EnsureSite(_ context.Context, site CDNSite) error {
	f.sites = append(f.sites, site)
	return nil
}

func init() {
	RegisterDNS("test-dns", func(lookup Lookup) (DNSProvider, error) {
		if lookup("EUDEPLOY_DNS_TEST_TOKEN") == "" {
			return nil, errors.New("set EUDEPLOY_DNS_TEST_TOKEN on the deploy runner")
		}
		return &fakeDNS{}, nil
	})
	RegisterCDN("test-cdn", func(Lookup) (CDNProvider, error) { return &fakeCDN{}, nil })
}

func lookupFrom(values map[string]string) Lookup {
	return func(name string) string { return values[name] }
}

func TestNoneMeansOff(t *testing.T) {
	for _, name := range []string{"", "none", " NONE "} {
		if err := ValidateName("dns", name); err != nil {
			t.Fatalf("ValidateName(%q) = %v", name, err)
		}
		dns, err := ResolveDNS(name, lookupFrom(nil))
		if err != nil || dns != nil {
			t.Fatalf("ResolveDNS(%q) = %v, %v; want nil, nil", name, dns, err)
		}
		cdn, err := ResolveCDN(name, lookupFrom(nil))
		if err != nil || cdn != nil {
			t.Fatalf("ResolveCDN(%q) = %v, %v; want nil, nil", name, cdn, err)
		}
	}
}

func TestApplyWithNothingConfiguredDoesNothing(t *testing.T) {
	// Even an unusable origin is fine: with no providers nothing is attempted.
	if err := Apply(context.Background(), nil, nil, []string{"bustora.lt"}, "not-an-ip"); err != nil {
		t.Fatalf("Apply with no providers = %v", err)
	}
}

func TestUnknownProviderNamesTheFix(t *testing.T) {
	err := ValidateName("dns", "cloudflare")
	if err == nil || !strings.Contains(err.Error(), `"none"`) || !strings.Contains(err.Error(), "test-dns") {
		t.Fatalf("unknown provider error should say how to turn it off and list options, got %v", err)
	}
	if _, err := ResolveCDN("bunny", lookupFrom(nil)); err == nil {
		t.Fatal("unregistered CDN should be rejected")
	}
}

func TestMissingCredentialFailsAtResolve(t *testing.T) {
	_, err := ResolveDNS("test-dns", lookupFrom(nil))
	if err == nil || !strings.Contains(err.Error(), "EUDEPLOY_DNS_TEST_TOKEN") {
		t.Fatalf("missing credential should name the variable, got %v", err)
	}
	dns, err := ResolveDNS("Test-DNS", lookupFrom(map[string]string{"EUDEPLOY_DNS_TEST_TOKEN": "x"}))
	if err != nil || dns == nil {
		t.Fatalf("configured provider should resolve case-insensitively: %v", err)
	}
}

func TestApplyEnsuresRecordsAndSites(t *testing.T) {
	dns := &fakeDNS{}
	cdn := &fakeCDN{}
	hostnames := []string{"bustora.lt", "BUSTORA.LT", "safebuild.lt", "", "89.167.126.100"}
	if err := Apply(context.Background(), dns, cdn, hostnames, "89.167.126.100"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantRecords := []DNSRecord{
		{Name: "bustora.lt", Type: "A", Value: "89.167.126.100"},
		{Name: "safebuild.lt", Type: "A", Value: "89.167.126.100"},
	}
	if len(dns.calls) != 1 || !reflect.DeepEqual(dns.calls[0], wantRecords) {
		t.Fatalf("records = %#v, want %#v", dns.calls, wantRecords)
	}
	wantSites := []CDNSite{{Hostname: "bustora.lt", OriginIP: "89.167.126.100"}, {Hostname: "safebuild.lt", OriginIP: "89.167.126.100"}}
	if !reflect.DeepEqual(cdn.sites, wantSites) {
		t.Fatalf("sites = %#v, want %#v", cdn.sites, wantSites)
	}
}

func TestApplyIPv6AndErrors(t *testing.T) {
	dns := &fakeDNS{}
	if err := Apply(context.Background(), dns, nil, []string{"a.lt"}, "2a01:4f9::1"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if dns.calls[0][0].Type != "AAAA" {
		t.Fatalf("IPv6 origin should create AAAA, got %#v", dns.calls[0])
	}
	if err := Apply(context.Background(), &fakeDNS{}, nil, []string{"a.lt"}, "server.example.com"); err == nil {
		t.Fatal("a hostname origin cannot become an A record")
	}
	failing := &fakeDNS{err: errors.New("quota")}
	if err := Apply(context.Background(), failing, nil, []string{"a.lt"}, "1.2.3.4"); err == nil || !strings.Contains(err.Error(), "fake-dns") {
		t.Fatalf("provider errors should name the provider, got %v", err)
	}
}

func TestRegisterRejectsReservedAndDuplicate(t *testing.T) {
	for _, name := range []string{"", "none", "test-dns"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("RegisterDNS(%q) should panic", name)
				}
			}()
			RegisterDNS(name, func(Lookup) (DNSProvider, error) { return nil, nil })
		}()
	}
}
