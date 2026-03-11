package grpcmiddleware

import (
	"context"
	"strconv"

	"github.com/amin/mesh-tenant-limiter/pkg/limiter"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor enforces tenant rate limits for unary gRPC calls.
func UnaryServerInterceptor(l *limiter.Limiter, metadataKey string) grpc.UnaryServerInterceptor {
	if metadataKey == "" {
		metadataKey = "x-tenant-id"
	}

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok || len(md.Get(metadataKey)) == 0 {
			return nil, status.Error(codes.InvalidArgument, "missing tenant metadata")
		}

		tenantID := md.Get(metadataKey)[0]
		decision, err := l.Check(ctx, tenantID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "rate limiter failure: %v", err)
		}

		headers := metadata.Pairs(
			"x-ratelimit-limit", strconv.Itoa(decision.Limit),
			"x-ratelimit-remaining", strconv.Itoa(decision.Remaining),
			"x-ratelimit-reset", strconv.FormatInt(decision.ResetAt.Unix(), 10),
		)
		if err := grpc.SetHeader(ctx, headers); err != nil {
			return nil, status.Errorf(codes.Internal, "set response headers: %v", err)
		}
		if !decision.Allowed {
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}

		return handler(ctx, req)
	}
}
