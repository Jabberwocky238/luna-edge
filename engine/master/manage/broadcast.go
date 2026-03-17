package manage

import (
	"context"
	"errors"
	"time"

	"github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

func noopBroadcast(context.Context, *Wrapper, any) error { return nil }

func publishDomain(ctx context.Context, w *Wrapper, domainID string) error {
	if batch := batchFromContext(ctx); batch != nil {
		batch.domains[domainID] = struct{}{}
		return nil
	}
	return publishDomainNow(ctx, w, domainID)
}

func publishRoute(ctx context.Context, w *Wrapper, domainID string, _ string) error {
	return publishDomain(ctx, w, domainID)
}

func publishCertificate(ctx context.Context, w *Wrapper, domainID string, _ uint64) error {
	return publishDomain(ctx, w, domainID)
}

func publishDeleteForDomain(ctx context.Context, w *Wrapper, domainID string, _ any, hostname string) error {
	if w == nil || w.publisher == nil {
		return nil
	}
	return w.publisher.PublishChangeLog(ctx, &engine.ChangeNotification{
		NodeID:    engine.POD_NAME,
		CreatedAt: time.Now().UTC(),
		DomainEntry: &metadata.DomainEntryProjection{
			ID:       domainID,
			Hostname: hostname,
			Deleted:  true,
		},
	})
}

func publishAllModel(ctx context.Context, w *Wrapper, model any) error {
	record, ok := model.(*metadata.DNSRecord)
	if !ok || record == nil {
		return nil
	}
	if batch := batchFromContext(ctx); batch != nil {
		batch.dnsRecords[record.ID] = struct{}{}
		return nil
	}
	return publishDNSRecordNow(ctx, w, record.ID)
}

func publishDNSRecordNow(ctx context.Context, w *Wrapper, recordID string) error {
	if w == nil || w.publisher == nil || recordID == "" {
		return nil
	}
	record := &metadata.DNSRecord{}
	if err := w.raw.DNSRecords().GetResourceByField(ctx, record, "id", recordID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return w.publisher.PublishChangeLog(ctx, &engine.ChangeNotification{
		NodeID:    engine.POD_NAME,
		CreatedAt: time.Now().UTC(),
		DNSRecord: record,
	})
}

func publishDomainNow(ctx context.Context, w *Wrapper, domainID string) error {
	if w == nil || w.publisher == nil || domainID == "" {
		return nil
	}
	domain, err := w.raw.GetDomainEndpointByID(ctx, domainID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if domain == nil {
		return nil
	}
	entry, err := w.raw.GetDomainEntryProjectionByDomain(ctx, domain.Hostname)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if entry == nil {
		return nil
	}
	return w.publisher.PublishChangeLog(ctx, &engine.ChangeNotification{
		NodeID:      engine.POD_NAME,
		CreatedAt:   time.Now().UTC(),
		DomainEntry: entry,
	})
}
