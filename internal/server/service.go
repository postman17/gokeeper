// Package server implements the GophKeeper gRPC service.
package server

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kmorozov/gophkeeper/internal/crypto"
	"github.com/kmorozov/gophkeeper/internal/server/auth"
	"github.com/kmorozov/gophkeeper/internal/server/blobstore"
	"github.com/kmorozov/gophkeeper/internal/server/storage"
	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
)

// GophKeeper implements the gophkeeper.v1.GophKeeper gRPC service.
type GophKeeper struct {
	pb.UnimplementedGophKeeperServer
	store          Storage
	jwt            *auth.JWTManager
	masterPassword string
	blobs          blobstore.BlobStore
}

// NewGophKeeper creates a GophKeeper service backed by the given storage and
// blob store. The master password is kept in memory only and is never logged
// or persisted.
func NewGophKeeper(store Storage, jwt *auth.JWTManager, masterPassword string, blobs blobstore.BlobStore) *GophKeeper {
	return &GophKeeper{store: store, jwt: jwt, masterPassword: masterPassword, blobs: blobs}
}

// Register creates a new user and returns a JWT token. Along with the user it
// generates a per-user DEK sealed with a KEK derived from the master password.
func (s *GophKeeper) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if req.GetLogin() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "login and password are required")
	}
	hash, err := auth.HashPassword(req.GetPassword())
	if err != nil {
		return nil, status.Error(codes.Internal, "hash password")
	}
	salt, err := crypto.NewSalt()
	if err != nil {
		return nil, status.Error(codes.Internal, "generate kek salt")
	}
	dek, err := crypto.NewDEK()
	if err != nil {
		return nil, status.Error(codes.Internal, "generate dek")
	}
	kek := crypto.DeriveKEK(s.masterPassword, salt)
	dekCiphertext, err := crypto.Encrypt(kek, dek)
	if err != nil {
		return nil, status.Error(codes.Internal, "wrap dek")
	}
	id, err := s.store.CreateUser(ctx, req.GetLogin(), hash, salt, dekCiphertext)
	if errors.Is(err, storage.ErrAlreadyExists) {
		return nil, status.Error(codes.AlreadyExists, "login is already taken")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "store user")
	}
	token, err := s.jwt.NewToken(id.String())
	if err != nil {
		return nil, status.Error(codes.Internal, "issue token")
	}
	return &pb.RegisterResponse{Token: token, UserId: id.String()}, nil
}

// Login authenticates an existing user and returns a JWT token. It also
// verifies that the stored DEK can be unwrapped with the current master
// password, so a mismatching server master password is detected early.
func (s *GophKeeper) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	user, err := s.store.GetUserByLogin(ctx, req.GetLogin())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.Unauthenticated, "invalid login or password")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "load user")
	}
	if !auth.CheckPassword(req.GetPassword(), user.PasswordHash) {
		return nil, status.Error(codes.Unauthenticated, "invalid login or password")
	}
	if _, err := s.unwrapDEK(user); err != nil {
		return nil, status.Error(codes.Internal, "decrypt dek: server master password may not match the one used at registration")
	}
	token, err := s.jwt.NewToken(user.ID.String())
	if err != nil {
		return nil, status.Error(codes.Internal, "issue token")
	}
	return &pb.LoginResponse{Token: token, UserId: user.ID.String()}, nil
}

// Ping checks connectivity and token validity.
func (s *GophKeeper) Ping(ctx context.Context, _ *emptypb.Empty) (*pb.PingResponse, error) {
	if _, ok := auth.UserIDFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "user is not authenticated")
	}
	if err := s.store.Ping(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "storage is unavailable")
	}
	return &pb.PingResponse{Ok: true}, nil
}

// CreateItem stores a new secret. Sensitive fields are sealed with the
// user's DEK before they reach the database.
func (s *GophKeeper) CreateItem(ctx context.Context, req *pb.CreateItemRequest) (*pb.CreateItemResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	item := req.GetItem()
	if item == nil {
		return nil, status.Error(codes.InvalidArgument, "item is required")
	}
	dek, err := s.getUserDEK(ctx, userID)
	if err != nil {
		return nil, err
	}
	login, err := crypto.EncryptString(dek, item.GetLogin())
	if err != nil {
		return nil, status.Error(codes.Internal, "encrypt login")
	}
	password, err := crypto.EncryptString(dek, item.GetPassword())
	if err != nil {
		return nil, status.Error(codes.Internal, "encrypt password")
	}
	data, err := crypto.Encrypt(dek, item.GetData())
	if err != nil {
		return nil, status.Error(codes.Internal, "encrypt data")
	}
	cardNumber, err := crypto.EncryptString(dek, item.GetCardNumber())
	if err != nil {
		return nil, status.Error(codes.Internal, "encrypt card number")
	}
	cardExp, err := crypto.EncryptString(dek, item.GetCardExp())
	if err != nil {
		return nil, status.Error(codes.Internal, "encrypt card exp")
	}
	cardCVV, err := crypto.EncryptString(dek, item.GetCardCvv())
	if err != nil {
		return nil, status.Error(codes.Internal, "encrypt card cvv")
	}
	meta, err := crypto.EncryptString(dek, item.GetMeta())
	if err != nil {
		return nil, status.Error(codes.Internal, "encrypt meta")
	}
	id, err := s.store.CreateItem(ctx, storage.Item{
		UserID:     userID,
		Type:       item.GetType().String(),
		Name:       item.GetName(),
		Login:      login,
		Password:   password,
		Data:       data,
		CardNumber: cardNumber,
		CardExp:    cardExp,
		CardCVV:    cardCVV,
		Meta:       meta,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "store item")
	}
	return &pb.CreateItemResponse{Id: id.String()}, nil
}

// GetItem returns a single secret by id with its fields decrypted.
func (s *GophKeeper) GetItem(ctx context.Context, req *pb.GetItemRequest) (*pb.GetItemResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(req.GetId())
	if err != nil {
		return nil, err
	}
	item, err := s.store.GetItem(ctx, userID, id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "item not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "load item")
	}
	dek, err := s.getUserDEK(ctx, userID)
	if err != nil {
		return nil, err
	}
	decrypted, err := s.decryptItem(dek, item)
	if err != nil {
		return nil, err
	}
	return &pb.GetItemResponse{Item: itemToProto(decrypted)}, nil
}

// ListItems returns all secrets of the current user with fields decrypted.
func (s *GophKeeper) ListItems(ctx context.Context, _ *pb.ListItemsRequest) (*pb.ListItemsResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListItems(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "load items")
	}
	dek, err := s.getUserDEK(ctx, userID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListItemsResponse{Items: make([]*pb.Item, 0, len(items))}
	for _, item := range items {
		decrypted, err := s.decryptItem(dek, item)
		if err != nil {
			return nil, err
		}
		resp.Items = append(resp.Items, itemToProto(decrypted))
	}
	return resp, nil
}

// DeleteItem removes a secret by id.
func (s *GophKeeper) DeleteItem(ctx context.Context, req *pb.DeleteItemRequest) (*pb.DeleteItemResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(req.GetId())
	if err != nil {
		return nil, err
	}
	deleted, err := s.store.DeleteItem(ctx, userID, id)
	if err != nil {
		return nil, status.Error(codes.Internal, "delete item")
	}
	if !deleted {
		return nil, status.Error(codes.NotFound, "item not found")
	}
	return &pb.DeleteItemResponse{Ok: true}, nil
}

// getUserDEK loads the user record and unwraps their DEK with a KEK derived
// from the current master password.
func (s *GophKeeper) getUserDEK(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "load user")
	}
	return s.unwrapDEK(user)
}

// unwrapDEK decrypts the stored DEK ciphertext with the KEK derived from the
// master password and the user's salt.
func (s *GophKeeper) unwrapDEK(user storage.User) ([]byte, error) {
	kek := crypto.DeriveKEK(s.masterPassword, user.KekSalt)
	dek, err := crypto.Decrypt(kek, user.DekCiphertext)
	if err != nil {
		return nil, status.Error(codes.Internal, "decrypt dek: server master password may not match the one used at registration")
	}
	return dek, nil
}

// decryptItem decrypts the sensitive fields of a stored item with the DEK.
func (s *GophKeeper) decryptItem(dek []byte, item storage.Item) (storage.Item, error) {
	var err error
	if item.Login, err = crypto.DecryptString(dek, item.Login); err != nil {
		return storage.Item{}, decryptError("login", err)
	}
	if item.Password, err = crypto.DecryptString(dek, item.Password); err != nil {
		return storage.Item{}, decryptError("password", err)
	}
	if item.Data, err = crypto.Decrypt(dek, item.Data); err != nil {
		return storage.Item{}, decryptError("data", err)
	}
	if item.CardNumber, err = crypto.DecryptString(dek, item.CardNumber); err != nil {
		return storage.Item{}, decryptError("card number", err)
	}
	if item.CardExp, err = crypto.DecryptString(dek, item.CardExp); err != nil {
		return storage.Item{}, decryptError("card exp", err)
	}
	if item.CardCVV, err = crypto.DecryptString(dek, item.CardCVV); err != nil {
		return storage.Item{}, decryptError("card cvv", err)
	}
	if item.Meta, err = crypto.DecryptString(dek, item.Meta); err != nil {
		return storage.Item{}, decryptError("meta", err)
	}
	return item, nil
}

// decryptError builds an Internal gRPC error for a field decryption failure.
func decryptError(field string, err error) error {
	return status.Errorf(codes.Internal, "decrypt item field %q: %v (server master password may not match the one used at registration)", field, err)
}

// userID extracts the authenticated user id from the context.
func (s *GophKeeper) userID(ctx context.Context) (uuid.UUID, error) {
	raw, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return uuid.Nil, status.Error(codes.Unauthenticated, "user is not authenticated")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, status.Error(codes.Unauthenticated, "invalid user id in token")
	}
	return id, nil
}

// parseID validates and parses an item id from a request.
func parseID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, status.Error(codes.InvalidArgument, "invalid item id")
	}
	return id, nil
}

// itemToProto converts a storage item into its protobuf representation.
func itemToProto(item storage.Item) *pb.Item {
	itemType := pb.ItemType_ITEM_TYPE_UNSPECIFIED
	if v, ok := pb.ItemType_value[item.Type]; ok {
		itemType = pb.ItemType(v)
	}
	return &pb.Item{
		Id:         item.ID.String(),
		Type:       itemType,
		Name:       item.Name,
		Login:      item.Login,
		Password:   item.Password,
		Data:       item.Data,
		CardNumber: item.CardNumber,
		CardExp:    item.CardExp,
		CardCvv:    item.CardCVV,
		Meta:       item.Meta,
		UpdatedAt:  timestamppb.New(item.UpdatedAt),
	}
}
