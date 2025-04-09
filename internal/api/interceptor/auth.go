package interceptor

import (
	"context"

	"connectrpc.com/connect"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func AuthInterceptor(storage storage.Storage, register chan model.DeviceStatusResponse) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			response, err := next(ctx, req)

			var connectErr *connect.Error
			if err != nil && errors.As(err, &connectErr) {
				if connect.CodeUnauthenticated != connectErr.Code() {
					log.Errorf("connect error: %v", connectErr.Message())
					log.Errorf("connect error code: %v", connectErr.Code())
					return response, err
				}

				storageErr := storage.Delete([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthExpireKey))
				if storageErr != nil {
					log.Errorf("failed to delete device auth: %v", storageErr)
				}

				storageErr = storage.Delete([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthKey))
				if storageErr != nil {
					log.Errorf("failed to delete device auth key: %v", storageErr)
				}

				register <- model.DeviceStatusResponse{
					Authorized: false,
					Exist:      true,
				}
				log.Infof("device auth expired, try to re-auth!")
			}

			return response, err
		}
	}
}
