package completion

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/pkg/querycache"
)

type snapshotLogWriter struct{ repo LogWriter }

type snapshotBestEffortLogWriter struct {
	snapshotLogWriter
	best BestEffortLogWriter
}

// SnapshotLogWriter 为每次存储调用复制记录，转为同步写入时重新复制同一输入。
func SnapshotLogWriter(repo LogWriter) LogWriter {
	if repo == nil {
		return nil
	}
	writer := snapshotLogWriter{repo: repo}
	if best, ok := repo.(BestEffortLogWriter); ok {
		return snapshotBestEffortLogWriter{snapshotLogWriter: writer, best: best}
	}
	return writer
}

func (w snapshotLogWriter) Create(ctx context.Context, row *UsageLog) (bool, error) {
	return w.repo.Create(ctx, querycache.Clone(row))
}

func (w snapshotBestEffortLogWriter) CreateBestEffort(ctx context.Context, row *UsageLog) error {
	return w.best.CreateBestEffort(ctx, querycache.Clone(row))
}
