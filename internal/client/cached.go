package client

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// CachedClient layers a local SQLite cache on top of the gRPC client.
// Read operations fall back to the cache when the server is unreachable.
type CachedClient struct {
	*GophKeeperClient
	cache   *cache.Cache
	timeout time.Duration
}

// NewCachedClient builds a CachedClient from the given base client and config.
// It uses cfg.CachePath for the local cache and cfg.Timeout to bound calls.
func NewCachedClient(base *GophKeeperClient, cfg Config) (*CachedClient, error) {
	c, err := cache.New(cfg.CachePath)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	return &CachedClient{GophKeeperClient: base, cache: c, timeout: timeout}, nil
}

// Close releases the cache database.
func (c *CachedClient) Close() error {
	return c.cache.Close()
}

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
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.GophKeeperClient.ListItems(callCtx)
	if err != nil {
		if !isUnavailable(err) {
			return nil, false, err
		}
		var pbItems []*pb.Item
		for it := range c.cache.ListItemsSeq(ctx) {
			pbItems = append(pbItems, cacheItemToPb(it))
		}
		if len(pbItems) == 0 {
			return nil, false, ErrNoCachedData
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
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
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

// UploadFile streams a file to the server; files are never cached locally,
// so this requires connectivity.
func (c *CachedClient) UploadFile(ctx context.Context, meta *pb.FileMeta, r io.Reader) (string, error) {
	id, err := c.GophKeeperClient.UploadFile(ctx, meta, r)
	if err != nil && isUnavailable(err) {
		return "", ErrServerUnavailable
	}
	return id, err
}

// DownloadFile streams a file from the server; files are never cached
// locally, so this requires connectivity.
func (c *CachedClient) DownloadFile(ctx context.Context, id string, onChunk func(*pb.FileMeta, []byte) error) error {
	err := c.GophKeeperClient.DownloadFile(ctx, id, onChunk)
	if err != nil && isUnavailable(err) {
		return ErrServerUnavailable
	}
	return err
}

// ListFiles fetches file records from the server; file metadata is not
// cached, so this requires connectivity.
func (c *CachedClient) ListFiles(ctx context.Context) ([]*pb.FileInfo, error) {
	resp, err := c.GophKeeperClient.ListFiles(ctx)
	if err != nil {
		if isUnavailable(err) {
			return nil, ErrServerUnavailable
		}
		return nil, err
	}
	return resp.GetFiles(), nil
}

// DeleteFile removes a stored file; it requires connectivity.
func (c *CachedClient) DeleteFile(ctx context.Context, id string) error {
	_, err := c.GophKeeperClient.DeleteFile(ctx, id)
	if err != nil {
		if isUnavailable(err) {
			return ErrServerUnavailable
		}
		return err
	}
	return nil
}
