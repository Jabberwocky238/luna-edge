package metadata

type DNSRecordType string

const (
	DNSTypeA     DNSRecordType = "A"
	DNSTypeAAAA  DNSRecordType = "AAAA"
	DNSTypeCNAME DNSRecordType = "CNAME"
	DNSTypeTXT   DNSRecordType = "TXT"
	DNSTypeMX    DNSRecordType = "MX"
	DNSTypeNS    DNSRecordType = "NS"
	DNSTypeSRV   DNSRecordType = "SRV"
	DNSTypeCAA   DNSRecordType = "CAA"
)

// DNSRecord 表示供 DNS 数据面直接查询的一条物化记录。
type DNSRecord struct {
	Shared
	// ID 是 DNS 记录对象的唯一标识。
	ID string `json:"id" gorm:"column:id;primaryKey;type:varchar(64)"`
	// FQDN 是该记录对应的完整域名。
	FQDN string `json:"fqdn" gorm:"column:fqdn;not null;index:idx_dns_lookup,priority:1;type:varchar(255)"`
	// RecordType 是该记录类型，例如 A、AAAA、TXT。
	RecordType DNSRecordType `json:"record_type" gorm:"column:record_type;not null;index:idx_dns_lookup,priority:2;type:varchar(32)"`
	// RoutingClass 表示该记录使用的路由类别，例如 simple 或 geo。
	RoutingClass string `json:"routing_class" gorm:"column:routing_class;not null;default:simple;type:varchar(32)"`
	// TTLSeconds 是该记录的 TTL 秒数。
	TTLSeconds uint32 `json:"ttl_seconds" gorm:"column:ttl_seconds;not null;default:60"`
	// ValuesJSON 是该记录值集合的 JSON 文本。
	ValuesJSON string `json:"values_json" gorm:"column:values_json;not null;default:'';type:text"`
	// RoutingKey 是高级路由场景下的附加键。
	RoutingKey string `json:"routing_key" gorm:"column:routing_key;not null;default:'';type:varchar(128)"`
	// Enabled 表示该记录当前是否参与应答。
	Enabled bool `json:"enabled" gorm:"column:enabled;not null;default:true"`
}

func (DNSRecord) TableName() string {
	return "dns_records"
}
