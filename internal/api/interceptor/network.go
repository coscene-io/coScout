package interceptor

import (
	"connectrpc.com/connect"
	"context"
	"github.com/coscene-io/coscout/internal/model"
	"google.golang.org/protobuf/proto"
)

func NetworkUsageInterceptor(networkChan chan *model.NetworkUsage) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			nu := model.NetworkUsage{}

			// Calculate the size of the request message
			if msg, ok := req.Any().(proto.Message); ok {
				nu.AddSent(int64(proto.Size(msg)))
			}

			// Call the handler
			res, err := next(ctx, req)

			if res != nil {
				if msg, ok := res.Any().(proto.Message); ok {
					nu.AddReceived(int64(proto.Size(msg)))
				}
			}

			networkChan <- &nu
			return res, err
		}
	}
}
