package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) SnapshotRecords() GenericRepository[*metadata.SnapshotRecord] {
	return &gormGenericRepository[*metadata.SnapshotRecord]{db: r.db}
}

func (r *GormRepository) AppendSnapshotRecord(ctx context.Context, record *metadata.SnapshotRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}
