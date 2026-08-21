package api

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GrpcUnarySentryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	hub := sentry.CurrentHub().Clone()
	hub.Scope().SetTag("grpc.method", info.FullMethod)
	ctx = sentry.SetHubOnContext(ctx, hub)

	defer func() {
		if r := recover(); r != nil {
			hub.Recover(r)
			hub.Flush(2 * time.Second)
			panic(r)
		}
	}()

	resp, err := handler(ctx, req)
	if code := status.Code(err); code == codes.Internal || code == codes.Unknown {
		hub.CaptureException(err)
	}
	return resp, err
}
