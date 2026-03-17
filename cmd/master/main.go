package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	master "github.com/jabberwocky238/luna-edge/engine/master"
	"github.com/jabberwocky238/luna-edge/repository/connection"
)

func main() {
	var cfg master.Config
	flag.Func("storage-driver", "master storage driver: postgres or sqlite", func(value string) error {
		cfg.StorageDriver = connection.Driver(value)
		return nil
	})
	flag.StringVar(&cfg.SQLitePath, "sqlite-path", envOr("LUNA_SQLITE_PATH", "./master.db"), "master sqlite path")
	flag.StringVar(&cfg.PostgresDSN, "postgres-dsn", envOr("LUNA_POSTGRES_DSN", ""), "master postgres dsn")
	flag.BoolVar(&cfg.AutoMigrate, "auto-migrate", envOr("LUNA_AUTO_MIGRATE", "") == "1", "run automigrate on startup")
	flag.StringVar(&cfg.S3.Endpoint, "s3-endpoint", envOr("LUNA_S3_ENDPOINT", ""), "s3 or minio endpoint")
	flag.StringVar(&cfg.S3.Region, "s3-region", envOr("LUNA_S3_REGION", ""), "s3 region")
	flag.StringVar(&cfg.S3.AccessKeyID, "s3-access-key-id", envOr("LUNA_S3_ACCESS_KEY_ID", ""), "s3 access key id")
	flag.StringVar(&cfg.S3.SecretAccessKey, "s3-secret-access-key", envOr("LUNA_S3_SECRET_ACCESS_KEY", ""), "s3 secret access key")
	flag.StringVar(&cfg.S3.SessionToken, "s3-session-token", envOr("LUNA_S3_SESSION_TOKEN", ""), "s3 session token")
	flag.BoolVar(&cfg.S3.UsePathStyle, "s3-path-style", envBool("LUNA_S3_PATH_STYLE", false), "use s3 path style access")
	flag.BoolVar(&cfg.S3.InsecureSkipVerify, "s3-insecure-skip-verify", envBool("LUNA_S3_INSECURE_SKIP_VERIFY", false), "skip s3 tls verification")
	flag.StringVar(&cfg.S3.StartupProbeBucket, "s3-startup-probe-bucket", envOr("LUNA_S3_STARTUP_PROBE_BUCKET", ""), "probe bucket on startup")
	flag.DurationVar(&cfg.S3.HTTPTimeout, "s3-http-timeout", envDuration("LUNA_S3_HTTP_TIMEOUT", 10*time.Second), "s3 http timeout")
	flag.BoolVar(&cfg.K8sDNSBridgeEnabled, "k8s-dns-bridge-enabled", envOr("LUNA_K8S_DNS_BRIDGE_ENABLED", "0") == "1", "enable kubernetes DnsDomainRecord bridge on master")
	flag.StringVar(&cfg.K8sNamespace, "k8s-namespace", envOr("LUNA_K8S_NAMESPACE", enginepkg.POD_NAMESPACE), "kubernetes namespace watched by master bridges")
	flag.StringVar(&cfg.ReplicationListenAddr, "replication-listen", envOr("LUNA_REPLICATION_LISTEN", ":50051"), "replication gRPC listen address")
	flag.StringVar(&cfg.ManageListenAddr, "manage-listen", envOr("LUNA_MANAGE_LISTEN", ":8080"), "manage HTTP listen address")
	flag.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", envDuration("LUNA_SHUTDOWN_TIMEOUT", 5*time.Second), "graceful shutdown timeout")
	if cfg.StorageDriver == "" {
		cfg.StorageDriver = connection.Driver(envOr("LUNA_STORAGE_DRIVER", string(connection.DriverPostgres)))
	}
	flag.Parse()

	engine, err := master.New(cfg)
	if err != nil {
		log.Fatalf("create master: %v", err)
	}
	if err := engine.Start(); err != nil {
		log.Fatalf("start master: %v", err)
	}
	log.Printf("master started: replication=%s manage=%s", cfg.ReplicationListenAddr, cfg.ManageListenAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := engine.Stop(shutdownCtx); err != nil {
		log.Fatalf("stop master: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
		if value == "1" {
			return true
		}
		if value == "0" {
			return false
		}
	}
	return fallback
}
