package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kmorozov/gophkeeper/internal/client/cache"
	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ErrNoCachedData is returned when the server is unreachable and the cache has no data.
var ErrNoCachedData = errors.New("server unavailable and no cached data")

// ErrServerUnavailable is returned for write operations when the server is unreachable.
var ErrServerUnavailable = errors.New("server unavailable")

// callTimeout bounds each gRPC call so an unreachable server fails fast.
// It can be overridden with the GOPHKEEPER_TIMEOUT environment variable
// (Go duration syntax, e.g. "30s" or "2m").
const callTimeout = 60 * time.Second

// envTimeoutVar is the environment variable overriding callTimeout.
const envTimeoutVar = "GOPHKEEPER_TIMEOUT"

// CachedClient layers a local SQLite cache on top of the gRPC client.
// Read operations fall back to the cache when the server is unreachable.
type CachedClient struct {
	*GophKeeperClient
	cache *cache.Cache
}

// NewCachedClient builds a CachedClient backed by the cache at the given path.
func NewCachedClient(base *GophKeeperClient, cachePath string) (*CachedClient, error) {
	c, err := cache.New(cachePath)
	if err != nil {
		return nil, err
	}
	return &CachedClient{GophKeeperClient: base, cache: c}, nil
}

// effectiveTimeout returns the call timeout, honoring the env override.
// It falls back to callTimeout when the override is unset or invalid.
func effectiveTimeout() time.Duration {
	if v := os.Getenv(envTimeoutVar); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return callTimeout
}

// Close releases the cache database.
func (c *CachedClient) Close() error {
	return c.cache.Close()
}

// isUnavailable reports whether the error means the server could not be reached.
// isUnavailable reports whether the error means the server could not be reached.
func isUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	}
	return false
}

// pbToCacheItem converts a protobuf item to its cache representation.
func pbToCacheItem(item *pb.Item) cache.Item {
	return cache.Item{
		ID:         item.GetId(),
		Type:       item.GetType(),
		Name:       item.GetName(),
		Login:      item.GetLogin(),
		Password:   item.GetPassword(),
		Data:       item.GetData(),
		CardNumber: item.GetCardNumber(),
		CardExp:    item.GetCardExp(),
		CardCvv:    item.GetCardCvv(),
		Meta:       item.GetMeta(),
		UpdatedAt:  item.GetUpdatedAt().AsTime(),
	}
}

// ListItems fetches all secrets from the server, refreshing the cache, and
// falls back to cached data when the server is unreachable.
func (c *CachedClient) ListItems(ctx context.Context) ([]*pb.Item, bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, effectiveTimeout())
	defer cancel()
	resp, err := c.GophKeeperClient.ListItems(callCtx)
	if err != nil {
		if !isUnavailable(err) {
			return nil, false, err
		}
		items, cerr := c.cache.ListItems(ctx)
		if cerr != nil {
			return nil, false, fmt.Errorf("read cache: %w", cerr)
		}
		if len(items) == 0 {
			return nil, false, ErrNoCachedData
		}
		pbItems := make([]*pb.Item, 0, len(items))
		for _, it := range items {
			pbItems = append(pbItems, cacheItemToPb(it))
		}
		return pbItems, true, nil
	}
	pbItems := resp.GetItems()
	cached := make([]cache.Item, 0, len(pbItems))
	for _, item := range pbItems {
		cached = append(cached, pbToCacheItem(item))
	}
	if err := c.cache.PutItems(ctx, cached); err != nil {
		return nil, false, fmt.Errorf("write cache: %w", err)
	}
	return pbItems, false, nil
}

// GetItem fetches a single secret from the server, refreshing the cache, and
// falls back to cached data when the server is unreachable.
func (c *CachedClient) GetItem(ctx context.Context, id string) (*pb.Item, bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, effectiveTimeout())
	defer cancel()
	resp, err := c.GophKeeperClient.GetItem(callCtx, id)
	if err != nil {
		if !isUnavailable(err) {
			return nil, false, err
		}
		item, ok, cerr := c.cache.GetItem(ctx, id)
		if cerr != nil {
			return nil, false, fmt.Errorf("read cache: %w", cerr)
		}
		if !ok {
			return nil, false, ErrNoCachedData
		}
		return cacheItemToPb(item), true, nil
	}
	item := resp.GetItem()
	if err := c.cache.PutItem(ctx, pbToCacheItem(item)); err != nil {
		return nil, false, fmt.Errorf("write cache: %w", err)
	}
	return item, false, nil
}

// CreateItem stores a new secret on the server; it requires connectivity.
func (c *CachedClient) CreateItem(ctx context.Context, item *pb.Item) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, effectiveTimeout())
	defer cancel()
	resp, err := c.GophKeeperClient.CreateItem(callCtx, item)
	if err != nil {
		if isUnavailable(err) {
			return "", ErrServerUnavailable
		}
		return "", err
	}
	return resp.GetId(), nil
}

// DeleteItem removes a secret on the server and drops it from the cache;
// it requires connectivity.
func (c *CachedClient) DeleteItem(ctx context.Context, id string) error {
	callCtx, cancel := context.WithTimeout(ctx, effectiveTimeout())
	defer cancel()
	if _, err := c.GophKeeperClient.DeleteItem(callCtx, id); err != nil {
		if isUnavailable(err) {
			return ErrServerUnavailable
		}
		return err
	}
	if err := c.cache.Delete(ctx, id); err != nil {
		return fmt.Errorf("evict cache: %w", err)
	}
	return nil
}

// cacheItemToPb converts a cached item back to its protobuf representation.
func cacheItemToPb(it cache.Item) *pb.Item {
	return &pb.Item{
		Id:         it.ID,
		Type:       it.Type,
		Name:       it.Name,
		Login:      it.Login,
		Password:   it.Password,
		Data:       it.Data,
		CardNumber: it.CardNumber,
		CardExp:    it.CardExp,
		CardCvv:    it.CardCvv,
		Meta:       it.Meta,
		UpdatedAt:  timestamppb.New(it.UpdatedAt),
	}
}
