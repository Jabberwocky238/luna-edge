package engine_test

import (
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestEngine1oNReplicationAndCatchUp(t *testing.T) {
	t.Run("fakekube", func(t *testing.T) {
		tc := newEngineTestCluster(t, 3)
		defer tc.Close()

		for i := 0; i < 3; i++ {
			tc.StartSlave(t, i)
		}

		tc.UpsertGatewayHTTPRoute(t, "mesh.example.com", "svc-a", 8080, 1)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveRoute("mesh.example.com", "svc-a", 8080)
		})

		tc.StopSlave(t, 0)
		tc.UpsertGatewayHTTPRoute(t, "mesh.example.com", "svc-b", 8081, 2)
		tc.StartSlave(t, 0)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveRoute("mesh.example.com", "svc-b", 8081)
		})

		tc.StopSlave(t, 1)
		tc.UpsertGatewayHTTPRoute(t, "mesh.example.com", "svc-c", 8082, 3)
		tc.StartSlave(t, 1)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveRoute("mesh.example.com", "svc-c", 8082)
		})

		tc.StopSlave(t, 2)
		tc.UpsertGatewayHTTPRoute(t, "mesh.example.com", "svc-d", 8083, 4)
		tc.StartSlave(t, 2)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveRoute("mesh.example.com", "svc-d", 8083)
		})
	})

	t.Run("certificates", func(t *testing.T) {
		tc := newEngineTestCluster(t, 3)
		defer tc.Close()

		tc.UpsertDomainBase(t, "multi-cert.example.com")
		for i := 0; i < 3; i++ {
			tc.StartSlave(t, i)
		}

		tc.StopSlave(t, 0)
		tc.UpsertCertificateAndBroadcast(t, "multi-cert.example.com", 1)
		tc.StartSlave(t, 0)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveCertificate("multi-cert.example.com", 1)
		})

		tc.StopSlave(t, 1)
		tc.UpsertCertificateAndBroadcast(t, "multi-cert.example.com", 2)
		tc.StartSlave(t, 1)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveCertificate("multi-cert.example.com", 2)
		})

		tc.StopSlave(t, 2)
		tc.UpsertCertificateAndBroadcast(t, "multi-cert.example.com", 3)
		tc.StartSlave(t, 2)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveCertificate("multi-cert.example.com", 3)
		})
	})

	t.Run("dns_records", func(t *testing.T) {
		tc := newEngineTestCluster(t, 3)
		defer tc.Close()

		for i := 0; i < 3; i++ {
			tc.StartSlave(t, i)
		}

		tc.StopSlave(t, 0)
		tc.UpsertDNSAndBroadcast(t, metadata.DNSRecord{
			ID:           "dns-a",
			FQDN:         "a.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["10.0.0.1"]`,
			Enabled:      true,
		})
		tc.StartSlave(t, 0)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveDNSRecord("a.example.com", `["10.0.0.1"]`)
		})

		tc.StopSlave(t, 1)
		tc.UpsertDNSAndBroadcast(t, metadata.DNSRecord{
			ID:           "dns-b",
			FQDN:         "b.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["10.0.0.2"]`,
			Enabled:      true,
		})
		tc.StartSlave(t, 1)
		tc.RequireEventually(t, func() error {
			if err := tc.AssertAllSlavesHaveDNSRecord("a.example.com", `["10.0.0.1"]`); err != nil {
				return err
			}
			return tc.AssertAllSlavesHaveDNSRecord("b.example.com", `["10.0.0.2"]`)
		})

		tc.StopSlave(t, 2)
		tc.UpsertDNSAndBroadcast(t, metadata.DNSRecord{
			ID:           "dns-c",
			FQDN:         "c.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["10.0.0.3"]`,
			Enabled:      true,
		})
		tc.StartSlave(t, 2)
		tc.RequireEventually(t, func() error {
			if err := tc.AssertAllSlavesHaveDNSRecord("a.example.com", `["10.0.0.1"]`); err != nil {
				return err
			}
			if err := tc.AssertAllSlavesHaveDNSRecord("b.example.com", `["10.0.0.2"]`); err != nil {
				return err
			}
			return tc.AssertAllSlavesHaveDNSRecord("c.example.com", `["10.0.0.3"]`)
		})
	})
}
