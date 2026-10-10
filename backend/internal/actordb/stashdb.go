package actordb

import "context"

// StashDBClient StashDB 客户端（TODO）
type StashDBClient struct {
	APIKey string
}

// PerformerInfo StashDB 演员信息
type PerformerInfo struct {
	ID       string
	Name     string
	Aliases  []string
	ImageURL string
	Bio      string
}

// SearchPerformer 按名字搜索演员（TODO）
func (c *StashDBClient) SearchPerformer(ctx context.Context, name string) (*PerformerInfo, error) {
	// TODO: 调用 StashDB GraphQL API
	return nil, nil
}
