package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/kmorozov/gophkeeper/internal/server/auth"
	"github.com/kmorozov/gophkeeper/internal/server/storage"
	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
)

const testMasterPassword = "test-master-password"

// fakeStorage is an in-memory implementation of Storage used by unit tests.
type fakeStorage struct {
	mu      sync.Mutex
	users   map[uuid.UUID]storage.User
	byLogin map[string]uuid.UUID
	items   map[uuid.UUID]storage.Item
	files   map[uuid.UUID]storage.File
}

// compile-time check that fakeStorage satisfies the consumer-side interface.
var _ Storage = (*fakeStorage)(nil)

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		users:   make(map[uuid.UUID]storage.User),
		byLogin: make(map[string]uuid.UUID),
		items:   make(map[uuid.UUID]storage.Item),
		files:   make(map[uuid.UUID]storage.File),
	}
}

func (f *fakeStorage) CreateUser(_ context.Context, login, passwordHash string, kekSalt, dekCiphertext []byte) (uuid.UUID, error) {
	if _, ok := f.byLogin[login]; ok {
		return uuid.Nil, storage.ErrAlreadyExists
	}
	id := uuid.New()
	f.users[id] = storage.User{
		ID:            id,
		Login:         login,
		PasswordHash:  passwordHash,
		KekSalt:       kekSalt,
		DekCiphertext: dekCiphertext,
		CreatedAt:     time.Now(),
	}
	f.byLogin[login] = id
	return id, nil
}

func (f *fakeStorage) GetUserByLogin(_ context.Context, login string) (storage.User, error) {
	id, ok := f.byLogin[login]
	if !ok {
		return storage.User{}, storage.ErrNotFound
	}
	return f.users[id], nil
}

func (f *fakeStorage) GetUserByID(_ context.Context, id uuid.UUID) (storage.User, error) {
	user, ok := f.users[id]
	if !ok {
		return storage.User{}, storage.ErrNotFound
	}
	return user, nil
}

func (f *fakeStorage) CreateItem(_ context.Context, item storage.Item) (uuid.UUID, error) {
	id := uuid.New()
	item.ID = id
	item.UpdatedAt = time.Now()
	f.items[id] = item
	return id, nil
}

func (f *fakeStorage) GetItem(_ context.Context, userID, id uuid.UUID) (storage.Item, error) {
	item, ok := f.items[id]
	if !ok || item.UserID != userID {
		return storage.Item{}, storage.ErrNotFound
	}
	return item, nil
}

func (f *fakeStorage) ListItems(_ context.Context, userID uuid.UUID) ([]storage.Item, error) {
	var items []storage.Item
	for _, item := range f.items {
		if item.UserID == userID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *fakeStorage) DeleteItem(_ context.Context, userID, id uuid.UUID) (bool, error) {
	item, ok := f.items[id]
	if !ok || item.UserID != userID {
		return false, nil
	}
	delete(f.items, id)
	return true, nil
}

func (f *fakeStorage) Ping(_ context.Context) error { return nil }

func (f *fakeStorage) Close() error { return nil }

func newTestService(t *testing.T) (*GophKeeper, *auth.JWTManager, *fakeStorage, *fakeBlobStore) {
	t.Helper()
	jwt := auth.NewJWTManager("test-secret", time.Hour)
	store := newFakeStorage()
	blobs := newFakeBlobStore()
	return NewGophKeeper(store, jwt, testMasterPassword, blobs), jwt, store, blobs
}

// authedCtx builds a context carrying a valid bearer token for userID, as the
// auth interceptor would produce for an authenticated call.
func authedCtx(t *testing.T, jwt *auth.JWTManager, userID string) context.Context {
	t.Helper()
	token, err := jwt.NewToken(userID)
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	incoming := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+token))
	var inner context.Context
	_, err = auth.NewUnaryInterceptor(jwt)(incoming, nil,
		&grpc.UnaryServerInfo{FullMethod: "/gophkeeper.v1.GophKeeper/GetItem"},
		func(ctx context.Context, _ any) (any, error) {
			inner = ctx
			return nil, nil
		})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	return inner
}

func TestRegisterSuccess(t *testing.T) {
	s, jwt, store, _ := newTestService(t)
	resp, err := s.Register(context.Background(), &pb.RegisterRequest{Login: "alice", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.GetToken() == "" {
		t.Fatal("expected non-empty token")
	}
	if _, err := jwt.ParseToken(resp.GetToken()); err != nil {
		t.Fatalf("token is not valid: %v", err)
	}
	user, err := store.GetUserByLogin(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetUserByLogin: %v", err)
	}
	if user.PasswordHash == "secret123" {
		t.Fatal("password must be stored hashed")
	}
	if len(user.KekSalt) == 0 || len(user.DekCiphertext) == 0 {
		t.Fatal("expected kek salt and wrapped dek to be stored")
	}
}

func TestRegisterAlreadyExists(t *testing.T) {
	s, _, _, _ := newTestService(t)
	ctx := context.Background()
	if _, err := s.Register(ctx, &pb.RegisterRequest{Login: "bob", Password: "secret123"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := s.Register(ctx, &pb.RegisterRequest{Login: "bob", Password: "other"})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("want AlreadyExists, got %v", err)
	}
}

func TestRegisterInvalidArgument(t *testing.T) {
	s, _, _, _ := newTestService(t)
	_, err := s.Register(context.Background(), &pb.RegisterRequest{Login: "", Password: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}

func TestLoginSuccess(t *testing.T) {
	s, jwt, _, _ := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, &pb.RegisterRequest{Login: "carol", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	resp, err := s.Login(ctx, &pb.LoginRequest{Login: "carol", Password: "secret123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.GetToken() == "" || resp.GetUserId() != reg.GetUserId() {
		t.Fatalf("unexpected login response: %+v", resp)
	}
	if _, err := jwt.ParseToken(resp.GetToken()); err != nil {
		t.Fatalf("token is not valid: %v", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	s, _, _, _ := newTestService(t)
	ctx := context.Background()
	if _, err := s.Register(ctx, &pb.RegisterRequest{Login: "carol", Password: "secret123"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err := s.Login(ctx, &pb.LoginRequest{Login: "carol", Password: "wrong"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestLoginUnknownUser(t *testing.T) {
	s, _, _, _ := newTestService(t)
	_, err := s.Login(context.Background(), &pb.LoginRequest{Login: "ghost", Password: "secret123"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestCreateItemUserNotFound(t *testing.T) {
	s, jwt, _, _ := newTestService(t)
	ctx := authedCtx(t, jwt, uuid.NewString())
	_, err := s.CreateItem(ctx, &pb.CreateItemRequest{Item: &pb.Item{Name: "note"}})
	if status.Code(err) != codes.Internal {
		t.Fatalf("want Internal, got %v", err)
	}
}

func TestCreateAndGetItem(t *testing.T) {
	s, jwt, store, _ := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, &pb.RegisterRequest{Login: "dave", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := reg.GetUserId()
	actx := authedCtx(t, jwt, userID)
	created, err := s.CreateItem(actx, &pb.CreateItemRequest{Item: &pb.Item{
		Type:     pb.ItemType_LOGIN,
		Name:     "example",
		Login:    "dave@example.com",
		Password: "hunter2",
	}})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if created.GetId() == "" {
		t.Fatal("expected non-empty item id")
	}
	stored, err := store.GetItem(ctx, uuid.MustParse(userID), uuid.MustParse(created.GetId()))
	if err != nil {
		t.Fatalf("storage GetItem: %v", err)
	}
	if stored.Login == "dave@example.com" {
		t.Fatal("login must be stored encrypted")
	}
	got, err := s.GetItem(actx, &pb.GetItemRequest{Id: created.GetId()})
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got.GetItem().GetLogin() != "dave@example.com" || got.GetItem().GetPassword() != "hunter2" {
		t.Fatalf("decrypted fields mismatch: %+v", got.GetItem())
	}
}

func TestGetItemNotFound(t *testing.T) {
	s, jwt, _, _ := newTestService(t)
	reg, err := s.Register(context.Background(), &pb.RegisterRequest{Login: "erin", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err = s.GetItem(authedCtx(t, jwt, reg.GetUserId()),
		&pb.GetItemRequest{Id: uuid.NewString()})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestGetItemUnauthenticated(t *testing.T) {
	s, _, _, _ := newTestService(t)
	_, err := s.GetItem(context.Background(), &pb.GetItemRequest{Id: uuid.NewString()})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestPingUnauthenticated(t *testing.T) {
	s, _, _, _ := newTestService(t)
	_, err := s.Ping(context.Background(), nil)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}
