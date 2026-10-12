package audit

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type blockingAudit struct {
	AuditLogRepository
	calls   atomic.Int64
	entered chan struct{}
	release chan struct{}
}

func (r *blockingAudit) BatchInsert(context.Context, []*AuditLog) (int64, error) {
	r.calls.Add(1)
	r.entered <- struct{}{}
	<-r.release
	return 100, nil
}

func TestRegressionAuditDuplicateStart(t *testing.T) {
	r := &blockingAudit{entered: make(chan struct{}, 4), release: make(chan struct{})}
	s := NewAuditLogService(r, nil)
	s.Start()
	s.Start()
	for range 200 {
		s.Record(&AuditLog{})
	}
	<-r.entered
	select {
	case <-r.entered:
		t.Error("duplicate Start created two concurrent audit writers")
	case <-time.After(100 * time.Millisecond):
	}
	close(r.release)
	s.Stop()
}
