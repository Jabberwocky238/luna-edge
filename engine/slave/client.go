package slave

import (
	"github.com/jabberwocky238/luna-edge/engine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type SlaveClient struct {
	grpcClient *grpc.ClientConn
	subscriber *streamSubscriber
}

func NewSlaveClient(cfg *Config) *SlaveClient {
	conn, err := grpc.NewClient(cfg.MasterAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil
	}
	client := engine.NewGRPCClient(conn)
	subscriber := &streamSubscriber{
		Client:  client,
		Applier: applier,
	}
	return &SlaveClient{
		grpcClient: conn,
		subscriber: subscriber,
	}
}
