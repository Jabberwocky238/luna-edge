package connection

import (
	"fmt"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type postgresConnection struct {
	db *gorm.DB
}

// OpenPostgres 创建一个 Postgres 连接。
func OpenPostgres(dsn string, autoMigrate bool) (Connection, error) {
	if dsn == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if autoMigrate {
		if err := metadata.AutoMigrate(db); err != nil {
			return nil, err
		}
	}

	return &postgresConnection{db: db}, nil
}

func (c *postgresConnection) DB() *gorm.DB {
	return c.db
}

func (c *postgresConnection) Driver() Driver {
	return DriverPostgres
}

func (c *postgresConnection) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
