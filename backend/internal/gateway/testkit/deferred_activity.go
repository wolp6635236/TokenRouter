package testkit

import (
	"context"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/infra/timingwheel"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// deferredActivityRepository 记录最终的存储写入，供测试检查网关行为。
type deferredActivityRepository struct {
	updates sync.Map
}

func (r *deferredActivityRepository) BatchUpdateLastUsed(_ context.Context, updates map[int64]time.Time) error {
	for id, ts := range updates {
		r.updates.Store(id, ts)
	}
	return nil
}

func DeferredActivityRecorder(t *testing.T) (*provider.DeferredService, *sync.Map) {
	t.Helper()
	wheel := timingwheel.New()
	repo := &deferredActivityRepository{}
	svc := provider.NewDeferredService(repo, wheel, provider.DeferredOptions{Interval: time.Second, Now: time.Now, Observe: log.Printf})
	t.Cleanup(func() { require.NoError(t, svc.StopContext(context.Background())) })
	return svc, &repo.updates
}
