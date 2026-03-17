package manage

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func noopBroadcast(context.Context, *Wrapper, any) error { return nil }

func publishDomain(ctx context.Context, w *Wrapper, domainID string) error {
	return publishNodesForDomain(ctx, w, domainID)
}

func publishAssignmentsForDomain(ctx context.Context, w *Wrapper, domainID string) error {
	return publishNodesForDomain(ctx, w, domainID)
}

func publishAssignmentsForNode(ctx context.Context, w *Wrapper, nodeID string) error {
	return publishNode(ctx, w, nodeID)
}

func publishRoute(ctx context.Context, w *Wrapper, domainID, _ string) error {
	return publishNodesForDomain(ctx, w, domainID)
}

func publishCertificate(ctx context.Context, w *Wrapper, domainID string, _ uint64) error {
	return publishNodesForDomain(ctx, w, domainID)
}

func publishDelete(ctx context.Context, w *Wrapper, nodeID string, _ any, _ string) error {
	return publishNode(ctx, w, nodeID)
}

func publishDeleteForDomain(ctx context.Context, w *Wrapper, domainID string, _ any, _ string) error {
	return publishNodesForDomain(ctx, w, domainID)
}

func publishNodesForDomain(ctx context.Context, w *Wrapper, domainID string) error {
	attachments, err := w.repo.ListAttachmentsByDomainID(ctx, domainID)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(attachments))
	for i := range attachments {
		nodeID := attachments[i].NodeID
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		if err := publishNode(ctx, w, nodeID); err != nil {
			return err
		}
	}
	return nil
}

func publishNode(ctx context.Context, w *Wrapper, nodeID string) error {
	if nodeID == "" || w.publisher == nil {
		return nil
	}
	return w.publisher.PublishNode(ctx, nodeID)
}
