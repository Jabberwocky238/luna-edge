package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) Nodes() GenericRepository[*metadata.Node] {
	return &gormGenericRepository[*metadata.Node]{db: r.db}
}

func (r *GormRepository) Attachments() GenericRepository[*metadata.Attachment] {
	return &gormGenericRepository[*metadata.Attachment]{db: r.db}
}

func (r *GormRepository) UpsertNode(ctx context.Context, node *metadata.Node) error {
	return r.Nodes().UpsertResource(ctx, node)
}

func (r *GormRepository) GetNode(ctx context.Context, nodeID string) (*metadata.Node, error) {
	node := &metadata.Node{}
	if err := r.Nodes().GetResourceByField(ctx, node, "id", nodeID); err != nil {
		return nil, err
	}
	return node, nil
}

func (r *GormRepository) ListNodes(ctx context.Context) ([]metadata.Node, error) {
	var nodes []metadata.Node
	err := r.Nodes().ListResource(ctx, &nodes, "id asc")
	return nodes, err
}

func (r *GormRepository) UpsertAttachment(ctx context.Context, attachment *metadata.Attachment) error {
	return r.Attachments().UpsertResource(ctx, attachment)
}

func (r *GormRepository) ListAttachmentsByNodeID(ctx context.Context, nodeID string) ([]metadata.Attachment, error) {
	var attachments []metadata.Attachment
	err := r.db.WithContext(ctx).Order("domain_id asc").Find(&attachments, "node_id = ?", nodeID).Error
	return attachments, err
}

func (r *GormRepository) ListAttachmentsByDomainID(ctx context.Context, domainID string) ([]metadata.Attachment, error) {
	var attachments []metadata.Attachment
	err := r.db.WithContext(ctx).Order("node_id asc").Find(&attachments, "domain_id = ?", domainID).Error
	return attachments, err
}
