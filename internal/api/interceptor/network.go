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
