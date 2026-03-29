package mailflow

import (
	"context"
	"sync"
)

// MemoryStateStore 用于单次命令内的瞬时事务状态，避免 process/debug 与长期运行状态互相污染。
type MemoryStateStore struct {
	mu     sync.Mutex
	states map[string]TxState
}

func (s *MemoryStateStore) Load(_ context.Context, key string) (TxState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.states == nil {
		return TxState{}, false, nil
	}
	state, ok := s.states[key]
	if !ok {
		return TxState{}, false, nil
	}
	return cloneTxState(state), true, nil
}

func (s *MemoryStateStore) Save(_ context.Context, state TxState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.states == nil {
		s.states = make(map[string]TxState)
	}
	s.states[state.Key] = cloneTxState(state)
	return nil
}

func cloneTxState(state TxState) TxState {
	cloned := state
	if len(state.Deliveries) > 0 {
		cloned.Deliveries = make(map[string]DeliveryReceipt, len(state.Deliveries))
		for key, receipt := range state.Deliveries {
			cloned.Deliveries[key] = receipt
		}
	}
	return cloned
}
