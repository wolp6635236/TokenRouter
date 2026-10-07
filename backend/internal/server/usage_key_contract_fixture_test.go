package server_test

import (
	"context"

	apikey "github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/usage/httpapi/ports"
)

// contractUsageKeys 将 APIKeyService 的查询和所有权检查接入用量接口测试。
func contractUsageKeys(keys *apikey.APIKeyService) ports.KeyReader {
	if keys == nil {
		return nil
	}
	return ports.KeyQueries{Lookup: func(ctx context.Context, id int64) (*ports.KeyReference, error) {
		v, e := keys.GetByID(ctx, id)
		if v == nil {
			return nil, e
		}
		return &ports.KeyReference{ID: v.ID, UserID: v.UserID, Name: v.Name}, e
	}, Ownership: keys.VerifyOwnership, Search: func(ctx context.Context, id int64, q string, n int) ([]ports.KeyReference, error) {
		rows, e := keys.SearchAPIKeys(ctx, id, q, n)
		if e != nil {
			return nil, e
		}
		out := make([]ports.KeyReference, len(rows))
		for i, v := range rows {
			out[i] = ports.KeyReference{ID: v.ID, UserID: v.UserID, Name: v.Name}
		}
		return out, nil
	}}
}
