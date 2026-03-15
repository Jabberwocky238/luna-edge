package connection

import (
	"fmt"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type sqliteConnection struct {
	db *gorm.DB
}

// OpenSQLite 创建一个 SQLite 连接。
func OpenSQLite(path string, autoMigrate bool) (Connection, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if autoMigrate {
		if err := metadata.AutoMigrate(db); err != nil {
			return nil, err
		}
	}

	return &sqliteConnection{db: db}, nil
}

func (c *sqliteConnection) DB() *gorm.DB {
	return c.db
}

func (c *sqliteConnection) Driver() Driver {
	return DriverSQLite
}

func (c *sqliteConnection) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
