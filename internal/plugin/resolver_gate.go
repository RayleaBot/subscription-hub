package plugin

import (
	"context"
	"sync"
	"time"
)

type resolverMediaGate struct {
	mu     sync.Mutex
	active int
}

func (gate *resolverMediaGate) acquire(ctx context.Context, limit int) (func(), error) {
	limit = boundedSetting(limit, 1, 1, 8)
	for {
		gate.mu.Lock()
		if gate.active < limit {
			gate.active++
			gate.mu.Unlock()
			return func() {
				gate.mu.Lock()
				gate.active--
				gate.mu.Unlock()
			}, nil
		}
		gate.mu.Unlock()

		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
