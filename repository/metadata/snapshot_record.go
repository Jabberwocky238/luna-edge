package metadata

import "time"

// DNSRecord / DomainEntryProjection
type SnapshotSyncType string

const (
	SnapshotSyncTypeDNSRecord             SnapshotSyncType = "DNSRecord"
	SnapshotSyncTypeDomainEntryProjection SnapshotSyncType = "DomainEntryProjection"
)

type SnapshotAction string

const (
	SnapshotActionDelete SnapshotAction = "delete"
	SnapshotActionUpsert SnapshotAction = "upsert"
)

// SnapshotRecord 表示一条用于增量同步的快照变更记录。
type SnapshotRecord struct {
	// ID 是自增主键。
	ID uint64 `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	// SyncType 表示同步对象类型。
	SyncType SnapshotSyncType `json:"sync_type" gorm:"column:sync_type;not null;type:text;index"`
	// SyncID 表示对应业务表中的对象 ID。
	SyncID string `json:"sync_id" gorm:"column:sync_id;not null;type:text;index"`
	// Action 表示此次同步动作。
	Action SnapshotAction `json:"action" gorm:"column:action;not null;type:text"`
	// CreatedAt 是该同步记录的创建时间。
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;not null;autoCreateTime"`
}

func (SnapshotRecord) TableName() string {
	return "snapshot_records"
}
