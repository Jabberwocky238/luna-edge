package functions

import (
	"context"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetDomainEntryProjectionByDomain(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := metadata.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	repo := &GormRepository{db: db}
	ctx := context.Background()

	domain := &metadata.DomainEndpoint{
		ID:          "domain-1",
		Hostname:    "app.example.com",
		BackendType: metadata.BackendTypeL7HTTP,
	}
	if err := db.WithContext(ctx).Create(domain).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}

	cert := &metadata.CertificateRevision{
		ID:               "cert-1",
		DomainEndpointID: "domain-1",
		Revision:         7,
		Provider:         "acme",
		ChallengeType:    metadata.ChallengeTypeHTTP01,
		ArtifactBucket:   "bucket-a",
		ArtifactPrefix:   "certs/app",
		SHA256Crt:        "crt-sha",
		SHA256Key:        "key-sha",
	}
	if err := db.WithContext(ctx).Create(cert).Error; err != nil {
		t.Fatalf("create cert: %v", err)
	}

	backend1 := &metadata.ServiceBackendRef{
		ID:               "backend-1",
		ServiceNamespace: "default",
		ServiceName:      "svc-a",
		ServicePort:      8080,
	}
	backend2 := &metadata.ServiceBackendRef{
		ID:               "backend-2",
		ServiceNamespace: "edge",
		ServiceName:      "svc-b",
		ServicePort:      9090,
	}
	if err := db.WithContext(ctx).Create(backend1).Error; err != nil {
		t.Fatalf("create backend1: %v", err)
	}
	if err := db.WithContext(ctx).Create(backend2).Error; err != nil {
		t.Fatalf("create backend2: %v", err)
	}

	route1 := &metadata.HTTPRoute{
		ID:               "route-1",
		DomainEndpointID: "domain-1",
		Path:             "/api",
		Priority:         20,
		BackendRefID:     "backend-1",
	}
	route2 := &metadata.HTTPRoute{
		ID:               "route-2",
		DomainEndpointID: "domain-1",
		Path:             "/",
		Priority:         10,
		BackendRefID:     "backend-2",
	}
	if err := db.WithContext(ctx).Create(route1).Error; err != nil {
		t.Fatalf("create route1: %v", err)
	}
	if err := db.WithContext(ctx).Create(route2).Error; err != nil {
		t.Fatalf("create route2: %v", err)
	}

	got, err := repo.GetDomainEntryProjectionByDomain(ctx, "app.example.com")
	if err != nil {
		t.Fatalf("GetDomainEntryProjectionByDomain: %v", err)
	}
	if got.ID != "domain-1" {
		t.Fatalf("unexpected domain id: %s", got.ID)
	}
	if got.Hostname != "app.example.com" {
		t.Fatalf("unexpected hostname: %s", got.Hostname)
	}
	if got.BackendType != metadata.BackendTypeL7HTTP {
		t.Fatalf("unexpected backend type: %s", got.BackendType)
	}
	if got.Cert == nil {
		t.Fatal("expected cert")
	}
	if got.Cert.ID != "cert-1" || got.Cert.Revision != 7 {
		t.Fatalf("unexpected cert: %+v", got.Cert)
	}
	if len(got.HTTPRoutes) != 2 {
		t.Fatalf("unexpected route count: %d", len(got.HTTPRoutes))
	}
	if got.HTTPRoutes[0].ID != "route-1" {
		t.Fatalf("unexpected first route order: %+v", got.HTTPRoutes)
	}
	if got.HTTPRoutes[0].BackendRef == nil || got.HTTPRoutes[0].BackendRef.ID != "backend-1" {
		t.Fatalf("unexpected first route backend: %+v", got.HTTPRoutes[0].BackendRef)
	}
	if got.HTTPRoutes[1].BackendRef == nil || got.HTTPRoutes[1].BackendRef.ID != "backend-2" {
		t.Fatalf("unexpected second route backend: %+v", got.HTTPRoutes[1].BackendRef)
	}
}

func TestGetDomainEntryProjectionByDomain_L4UsesBindedBackendRef(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := metadata.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	repo := &GormRepository{db: db}
	ctx := context.Background()

	backend := &metadata.ServiceBackendRef{
		ID:               "backend-l4",
		ServiceNamespace: "default",
		ServiceName:      "svc-l4",
		ServicePort:      443,
	}
	if err := db.WithContext(ctx).Create(backend).Error; err != nil {
		t.Fatalf("create backend: %v", err)
	}

	domain := &metadata.DomainEndpoint{
		ID:              "domain-l4",
		Hostname:        "tcp.example.com",
		BackendType:     metadata.BackendTypeL4TLSPassthrough,
		BindedServiceID: "backend-l4",
	}
	if err := db.WithContext(ctx).Create(domain).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}

	got, err := repo.GetDomainEntryProjectionByDomain(ctx, "tcp.example.com")
	if err != nil {
		t.Fatalf("GetDomainEntryProjectionByDomain: %v", err)
	}
	if got.BindedBackendRef == nil {
		t.Fatal("expected binded backend ref")
	}
	if got.BindedBackendRef.ID != "backend-l4" {
		t.Fatalf("unexpected binded backend ref: %+v", got.BindedBackendRef)
	}
	if len(got.HTTPRoutes) != 0 {
		t.Fatalf("expected no http routes for l4 domain, got: %d", len(got.HTTPRoutes))
	}
}
