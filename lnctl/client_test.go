package lnctl

import (
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func TestValidateResource(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if err := validateResource(""); err == nil {
			t.Fatal("expected error for empty resource")
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		if err := validateResource("unknown"); err == nil {
			t.Fatal("expected error for unsupported resource")
		}
	})

	t.Run("supported", func(t *testing.T) {
		if err := validateResource("zones"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateRequiredID(t *testing.T) {
	if err := validateRequiredID("zones", ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := validateRequiredID("zones", "zone-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPutZoneRequiresID(t *testing.T) {
	client := NewClient("http://127.0.0.1:8080")
	if _, err := client.Zones().Put(metadata.Zone{}); err == nil {
		t.Fatal("expected error for missing id")
	}
}
