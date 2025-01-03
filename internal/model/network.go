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

package model

import (
	"sync/atomic"
)

type NetworkUsage struct {
	TotalSent     atomic.Int64
	TotalReceived atomic.Int64
}

func (nu *NetworkUsage) AddSent(n int64) {
	nu.TotalSent.Add(n)
}

func (nu *NetworkUsage) AddReceived(n int64) {
	nu.TotalReceived.Add(n)
}

func (nu *NetworkUsage) GetTotalSent() int64 {
	return nu.TotalSent.Load()
}

func (nu *NetworkUsage) GetTotalReceived() int64 {
	return nu.TotalReceived.Load()
}

func (nu *NetworkUsage) ReduceSent(n int64) int64 {
	return nu.TotalSent.Add(-n)
}

func (nu *NetworkUsage) ReduceReceived(n int64) int64 {
	return nu.TotalReceived.Add(-n)
}

func (nu *NetworkUsage) Reset() {
	nu.TotalSent.Store(0)
	nu.TotalReceived.Store(0)
}
