// Command server runs the GophKeeper gRPC server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/kmorozov/gophkeeper/internal/server"
	"github.com/kmorozov/gophkeeper/internal/server/auth"
	"github.com/kmorozov/gophkeeper/internal/server/blobstore"
	"github.com/kmorozov/gophkeeper/internal/server/storage"
	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
)

// defaultServerAddr is the address the gRPC server listens on.
const defaultServerAddr = ":3200"

// Development defaults allow `go run ./cmd/server` to start against a
// local docker compose stack (postgres on localhost:5437, minio on
// localhost:9000) without any flags or environment variables. They must
// never be used in production: the server logs a warning when they apply.
const (
	defaultDevDSN            = "postgres://gophkeeper:gophkeeper@localhost:5437/gophkeeper?sslmode=disable"
	defaultDevJWTSecret      = "dev-secret"
	defaultDevMasterPassword = "dev-master"
)

func main() {
	dsn := flag.String("d", os.Getenv("DATABASE_URL"), "PostgreSQL DSN (or DATABASE_URL)")
	jwtSecret := flag.String("s", os.Getenv("JWT_SECRET"), "JWT signing secret (or JWT_SECRET)")
	addr := flag.String("a", defaultServerAddr, "gRPC listen address")
	masterPassword := flag.String("master-password", os.Getenv("GOPHKEEPER_MASTER_PASSWORD"), "master password for secret encryption (or GOPHKEEPER_MASTER_PASSWORD)")
	tokenTTL := flag.String("token-ttl", envOr("GOPHKEEPER_TOKEN_TTL", "24h"), "JWT token lifetime (or GOPHKEEPER_TOKEN_TTL), e.g. 24h, 30m")
	flag.Parse()

	if *dsn == "" {
		log.Printf("WARNING: DATABASE_URL is not set, using a development default (postgres on localhost:5437); do not use in production")
		*dsn = defaultDevDSN
	}
	if *jwtSecret == "" {
		log.Printf("WARNING: JWT_SECRET is not set, using an insecure development default; do not use in production")
		*jwtSecret = defaultDevJWTSecret
	}
	if *masterPassword == "" {
		log.Printf("WARNING: GOPHKEEPER_MASTER_PASSWORD is not set, using an insecure development default; do not use in production")
		*masterPassword = defaultDevMasterPassword
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewPostgresStorage(ctx, *dsn)
	if err != nil {
		log.Fatalf("init storage: %v", err)
	}
	defer store.Close()
	ttl, err := time.ParseDuration(*tokenTTL)
	if err != nil || ttl <= 0 {
		log.Fatalf("invalid token TTL %q: use a positive duration like 24h or 30m", *tokenTTL)
	}
	jwt := auth.NewJWTManager(*jwtSecret, ttl)

	blobs, err := blobstore.NewS3BlobStore(ctx, blobstore.S3Config{
		Endpoint:  envOr("S3_ENDPOINT", "http://localhost:9000"),
		Bucket:    envOr("S3_BUCKET", "gophkeeper"),
		AccessKey: envOr("S3_ACCESS_KEY", "gophkeeper"),
		SecretKey: envOr("S3_SECRET_KEY", "gophkeeper"),
	})
	if err != nil {
		log.Fatalf("init blob store: %v", err)
	}
	if err := blobs.EnsureBucket(ctx); err != nil {
		log.Fatalf("ensure bucket: %v", err)
	}

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		auth.NewUnaryInterceptor(jwt),
	), grpc.ChainStreamInterceptor(
		auth.NewStreamInterceptor(jwt),
	))
	pb.RegisterGophKeeperServer(grpcServer, server.NewGophKeeper(store, jwt, *masterPassword, blobs))

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("gophkeeper server listening on %s\n", *addr)
		errCh <- grpcServer.Serve(lis)
	}()

	select {
	case err := <-errCh:
		log.Fatalf("serve: %v", err)
	case <-ctx.Done():
		grpcServer.GracefulStop()
	}
}

// envOr returns the environment variable value or the fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
