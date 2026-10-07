package provider_test

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// 影子测试通过内存替身读写提供商记录。
type crsShadowStore struct {
	rows map[int64]*provider.Record
}

func newCRSShadowStore() *crsShadowStore {
	return &crsShadowStore{rows: make(map[int64]*provider.Record)}
}

func (s *crsShadowStore) Create(_ context.Context, record *provider.Record) error {
	record.ID = int64(len(s.rows) + 1)
	s.rows[record.ID] = record
	return nil
}

func (s *crsShadowStore) GetByID(_ context.Context, id int64) (*provider.Record, error) {
	return s.rows[id], nil
}

func (s *crsShadowStore) ListShadowsByParent(_ context.Context, id int64) ([]*provider.Record, error) {
	var rows []*provider.Record
	for _, row := range s.rows {
		if row.ParentProviderID != nil && *row.ParentProviderID == id {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (s *crsShadowStore) Update(_ context.Context, record *provider.Record) error {
	s.rows[record.ID] = record
	return nil
}

func (s *crsShadowStore) UpdateConfiguration(ctx context.Context, record *provider.Record, _ provider.ConfigurationChange) error {
	return s.Update(ctx, record)
}
