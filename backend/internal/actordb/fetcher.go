package actordb

import "context"

// ActorMetadata 演员元数据快照（来自任意数据源）
type ActorMetadata struct {
	AvatarURL    string
	Bio          string
	EmbyPersonID string
	// Works 可选：数据源返回的作品列表（用于补全 ActorWork.EmbyItemID）
	Works []ActorWorkInfo
}

type ActorWorkInfo struct {
	EmbyItemID string
	Code       string
	Title      string
}

// ActorFetcher 演员元数据获取接口
// 后续可以加 EmbyFetcher / TMDBFetcher / JavBusFetcher 等多种实现
type ActorFetcher interface {
	Name() string
	Fetch(ctx context.Context, actor *ActorProfile) (*ActorMetadata, error)
}

// ============ 默认实现：NoopFetcher ============
// 什么都不做，用于当前阶段先把框架跑起来

type NoopFetcher struct{}

func (NoopFetcher) Name() string { return "noop" }

func (NoopFetcher) Fetch(ctx context.Context, actor *ActorProfile) (*ActorMetadata, error) {
	return &ActorMetadata{}, nil
}

// ============ 预留：EmbyFetcher（空壳，后面填） ============

type EmbyFetcher struct {
	BaseURL string
	APIKey  string
}

func NewEmbyFetcher(baseURL, apiKey string) *EmbyFetcher {
	return &EmbyFetcher{BaseURL: baseURL, APIKey: apiKey}
}

func (f *EmbyFetcher) Name() string { return "emby" }

func (f *EmbyFetcher) Fetch(ctx context.Context, actor *ActorProfile) (*ActorMetadata, error) {
	// TODO: 后面实现。当前直接返回空结果，避免阻塞。
	// 参考接口：
	//   GET /Persons?SearchTerm={name}
	//   GET /Persons/{id}
	//   GET /Persons/{id}/Images/Primary
	return &ActorMetadata{}, nil
}
