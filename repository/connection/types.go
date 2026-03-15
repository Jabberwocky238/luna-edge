package connection

import "gorm.io/gorm"

// Driver 表示数据库驱动类型。
type Driver string

const (
	// DriverSQLite 表示 SQLite 驱动。
	DriverSQLite Driver = "sqlite"
	// DriverPostgres 表示 Postgres 驱动。
	DriverPostgres Driver = "postgres"
)

// Connection 是数据库连接的统一抽象。
type Connection interface {
	// DB 返回底层 GORM 数据库句柄。
	DB() *gorm.DB
	// Driver 返回当前连接使用的驱动类型。
	Driver() Driver
	// Close 关闭底层数据库连接。
	Close() error
}

// Config 是创建数据库连接时使用的统一配置。
type Config struct {
	// Driver 指定目标数据库驱动。
	Driver Driver
	// DSN 是数据库连接字符串；Postgres 必填。
	DSN string
	// Path 是 SQLite 数据文件路径；SQLite 模式使用。
	Path string
	// AutoMigrate 表示连接建立后是否执行自动迁移。
	AutoMigrate bool
}
