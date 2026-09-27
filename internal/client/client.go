package client

import (
	"context"
	"fmt"

	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

// GophKeeperClient wraps the generated gRPC client with token handling.
type GophKeeperClient struct {
	conn   *grpc.ClientConn
	raw    pb.GophKeeperClient
	tokens *TokenStore
}

// NewGophKeeperClient dials the server and returns a ready-to-use client.
func NewGophKeeperClient(cfg Config) (*GophKeeperClient, error) {
	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cfg.Addr, err)
	}
	return &GophKeeperClient{
		conn:   conn,
		raw:    pb.NewGophKeeperClient(conn),
		tokens: NewTokenStore(cfg.TokenPath),
	}, nil
}

// Close releases the underlying connection.
func (c *GophKeeperClient) Close() error {
	return c.conn.Close()
}

// Tokens exposes the token store of this client.
func (c *GophKeeperClient) Tokens() *TokenStore {
	return c.tokens
}

// withToken attaches the stored authorization token to the context.
func (c *GophKeeperClient) withToken(ctx context.Context) (context.Context, error) {
	rec, err := c.tokens.Load()
	if err != nil {
		return nil, err
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+rec.Token), nil
}

// Register creates a new user on the server.
func (c *GophKeeperClient) Register(ctx context.Context, login, password string) (*pb.RegisterResponse, error) {
	return c.raw.Register(ctx, &pb.RegisterRequest{Login: login, Password: password})
}

// Login authenticates an existing user on the server.
func (c *GophKeeperClient) Login(ctx context.Context, login, password string) (*pb.LoginResponse, error) {
	return c.raw.Login(ctx, &pb.LoginRequest{Login: login, Password: password})
}

// Ping checks connectivity and token validity.
func (c *GophKeeperClient) Ping(ctx context.Context) (*pb.PingResponse, error) {
	ctx, err := c.withToken(ctx)
	if err != nil {
		return nil, err
	}
	return c.raw.Ping(ctx, &emptypb.Empty{})
}

// CreateItem stores a new secret on the server.
func (c *GophKeeperClient) CreateItem(ctx context.Context, item *pb.Item) (*pb.CreateItemResponse, error) {
	ctx, err := c.withToken(ctx)
	if err != nil {
		return nil, err
	}
	return c.raw.CreateItem(ctx, &pb.CreateItemRequest{Item: item})
}

// GetItem fetches a single secret by id.
func (c *GophKeeperClient) GetItem(ctx context.Context, id string) (*pb.GetItemResponse, error) {
	ctx, err := c.withToken(ctx)
	if err != nil {
		return nil, err
	}
	return c.raw.GetItem(ctx, &pb.GetItemRequest{Id: id})
}

// ListItems fetches all secrets of the current user.
func (c *GophKeeperClient) ListItems(ctx context.Context) (*pb.ListItemsResponse, error) {
	ctx, err := c.withToken(ctx)
	if err != nil {
		return nil, err
	}
	return c.raw.ListItems(ctx, &pb.ListItemsRequest{})
}

// DeleteItem removes a secret by id.
func (c *GophKeeperClient) DeleteItem(ctx context.Context, id string) (*pb.DeleteItemResponse, error) {
	ctx, err := c.withToken(ctx)
	if err != nil {
		return nil, err
	}
	return c.raw.DeleteItem(ctx, &pb.DeleteItemRequest{Id: id})
}
