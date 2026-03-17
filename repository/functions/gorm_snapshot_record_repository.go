package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) SnapshotRecords() GenericRepository[*metadata.SnapshotRecord] {
	return &GormGenericRepository[*metadata.SnapshotRecord]{db: r.db}
}

func (r *GormRepository) AppendSnapshotRecord(ctx context.Context, record *metadata.SnapshotRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}

func (r *GormRepository) ListSnapshotRecordsAfter(ctx context.Context, afterID uint64) ([]metadata.SnapshotRecord, error) {
	var records []metadata.SnapshotRecord
	err := r.db.WithContext(ctx).
		Order("id asc").
		Find(&records, "id > ?", afterID).Error
	return records, err
}
