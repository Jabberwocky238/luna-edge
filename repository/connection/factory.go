package connection

import "fmt"

// Open 根据配置创建具体数据库连接。
func Open(cfg Config) (Connection, error) {
	switch cfg.Driver {
	case DriverSQLite:
		return OpenSQLite(cfg.Path, cfg.AutoMigrate)
	case DriverPostgres:
		return OpenPostgres(cfg.DSN, cfg.AutoMigrate)
	default:
		return nil, fmt.Errorf("unsupported driver %q", cfg.Driver)
	}
}
