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
