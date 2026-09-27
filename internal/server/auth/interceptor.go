package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// metadataKeyAuthorization is the gRPC metadata key carrying the JWT token.
const metadataKeyAuthorization = "authorization"

// contextKeyUserID is the context key for the authenticated user ID.
type contextKeyUserID struct{}

// UserIDFromContext returns the user ID stored by the auth interceptor.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(contextKeyUserID{}).(string)
	return userID, ok
}

// publicMethods is the set of full method names that do not require authentication.
var publicMethods = map[string]bool{
	"/gophkeeper.v1.GophKeeper/Register": true,
	"/gophkeeper.v1.GophKeeper/Login":    true,
}

// NewUnaryInterceptor returns a server interceptor that verifies JWT tokens
// from the "authorization" metadata for all methods except Register and Login.
func NewUnaryInterceptor(jwt *JWTManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if publicMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		token := tokenFromContext(ctx)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing authorization token")
		}
		userID, err := jwt.ParseToken(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization token")
		}
		ctx = context.WithValue(ctx, contextKeyUserID{}, userID)
		return handler(ctx, req)
	}
}

// tokenFromContext extracts the bearer token from incoming metadata.
func tokenFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(metadataKeyAuthorization)
	if len(values) == 0 {
		return ""
	}
	token := values[0]
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	return strings.TrimSpace(token)
}
