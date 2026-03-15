package repository

import (
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/functions"
)

// Repository 聚合所有仓储操作接口。
type Repository interface {
	functions.Repository
}

// Factory 表示仓储工厂。
type Factory interface {
	// Connection 返回底层数据库连接。
	Connection() connection.Connection
	// Repository 返回聚合后的仓储接口。
	Repository() Repository
	// Close 关闭底层连接。
	Close() error
}

type factory struct {
	conn connection.Connection
	repo functions.Repository
}

// NewFactory 根据数据库配置创建一个聚合仓储工厂。
func NewFactory(cfg connection.Config) (Factory, error) {
	conn, err := connection.Open(cfg)
	if err != nil {
		return nil, err
	}

	return &factory{
		conn: conn,
		repo: functions.NewGormRepository(conn.DB()),
	}, nil
}

func (f *factory) Connection() connection.Connection {
	return f.conn
}

func (f *factory) Repository() Repository {
	return f.repo
}

func (f *factory) Close() error {
	return f.conn.Close()
}
