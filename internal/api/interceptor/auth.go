// Copyright 2025 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
