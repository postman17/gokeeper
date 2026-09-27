package cache

import (
	"context"
	"testing"
	"time"

	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := New(t.TempDir() + "/cache.db")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func item(id string, updated time.Time) Item {
	return Item{
		ID:        id,
		Type:      pb.ItemType_LOGIN,
		Name:      "name-" + id,
		Login:     "login-" + id,
		Password:  "pass-" + id,
		Data:      []byte("data-" + id),
		Meta:      "meta-" + id,
		UpdatedAt: updated,
	}
}

func TestPutGetRoundtrip(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	want := item("a", time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
	if err := c.PutItem(ctx, want); err != nil {
		t.Fatalf("PutItem: %v", err)
	}
	got, ok, err := c.GetItem(ctx, "a")
	if err != nil || !ok {
		t.Fatalf("GetItem: ok=%v err=%v", ok, err)
	}
	if got.ID != want.ID || got.Type != want.Type || got.Name != want.Name ||
		got.Login != want.Login || got.Password != want.Password ||
		string(got.Data) != string(want.Data) || got.Meta != want.Meta ||
		!got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}

func TestGetMissing(t *testing.T) {
	c := newTestCache(t)
	if _, ok, err := c.GetItem(context.Background(), "nope"); err != nil || ok {
		t.Fatalf("GetItem missing: ok=%v err=%v", ok, err)
	}
}

func TestPutItemsUpsert(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := c.PutItems(ctx, []Item{item("a", base), item("b", base.Add(time.Hour))}); err != nil {
		t.Fatalf("PutItems: %v", err)
	}
	revised := item("a", base.Add(2*time.Hour))
	revised.Name = "renamed"
	if err := c.PutItems(ctx, []Item{revised}); err != nil {
		t.Fatalf("PutItems upsert: %v", err)
	}
	got, ok, err := c.GetItem(ctx, "a")
	if err != nil || !ok {
		t.Fatalf("GetItem: ok=%v err=%v", ok, err)
	}
	if got.Name != "renamed" || got.UpdatedAt != revised.UpdatedAt {
		t.Fatalf("upsert mismatch: %+v", got)
	}
	items, err := c.ListItems(ctx)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
}

func TestListOrderingByUpdatedAtDesc(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := c.PutItems(ctx, []Item{item("old", base), item("new", base.Add(24*time.Hour)), item("mid", base.Add(time.Hour))}); err != nil {
		t.Fatalf("PutItems: %v", err)
	}
	items, err := c.ListItems(ctx)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	wantOrder := []string{"new", "mid", "old"}
	if len(items) != len(wantOrder) {
		t.Fatalf("want %d items, got %d", len(wantOrder), len(items))
	}
	for i, id := range wantOrder {
		if items[i].ID != id {
			t.Fatalf("item %d: want id %s, got %s", i, id, items[i].ID)
		}
	}
}

func TestDelete(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	if err := c.PutItem(ctx, item("a", time.Now())); err != nil {
		t.Fatalf("PutItem: %v", err)
	}
	if err := c.Delete(ctx, "a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := c.GetItem(ctx, "a"); err != nil || ok {
		t.Fatalf("GetItem after delete: ok=%v err=%v", ok, err)
	}
	if err := c.Delete(ctx, "missing"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}
