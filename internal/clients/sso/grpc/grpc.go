package grpc

import (
	"context"
	"fmt"
	"time"

	ssov1 "github.com/Vladislav747/protos/gen/go/sso"
	grpcLogging "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"golang.org/x/exp/slog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	api ssov1.AuthClient

	log *slog.Logger
}

func New(
	ctx context.Context,
	log *slog.Logger,
	addr string,
	timeout time.Duration,
	retriesCount int,
) (*Client, error) {
	retryOpts := []retry.CallOption{
		retry.WithCodes(codes.NotFound, codes.Aborted, codes.DeadlineExceeded, codes.Internal, codes.Unavailable),
		retry.WithMax(uint(retriesCount)),
		retry.WithPerRetryTimeout(timeout),
	}

	logOpts := []grpcLogging.Option{
		grpcLogging.WithLogOnEvents(grpcLogging.PayloadReceived, grpcLogging.PayloadSent),
	}

	cc, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			grpcLogging.UnaryClientInterceptor(InterceptorLogger(log), logOpts...),
			retry.UnaryClientInterceptor(retryOpts...),
		),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		api: ssov1.NewAuthClient(cc),
		log: log,
	}, nil
}

func (c *Client) isAdmin(ctx context.Context, userID int64) (bool, error) {
	const op = "grpc.isAdmin"

	resp, err := c.api.IsAdmin(ctx, &ssov1.IsAdminRequest{
		UserId: userID,
	})
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	return resp.IsAdmin, nil
}

func InterceptorLogger(l *slog.Logger) grpcLogging.Logger {
	return grpcLogging.LoggerFunc(func(ctx context.Context, lvl grpcLogging.Level, msg string, fields ...any) {
		l.Log(ctx, slog.Level(lvl), msg, fields...)
	})
}
