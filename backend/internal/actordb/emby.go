package actordb

import "context"

// EmbyClient Emby 客户端（TODO）
type EmbyClient struct {
	BaseURL string
	APIKey  string
}

// PushPerson 推送演员到 Emby（TODO）
func (c *EmbyClient) PushPerson(ctx context.Context, p *ActorProfile) error {
	// TODO: 上传头像 / 写 person.nfo
	return nil
}

// TestConnection 测试连接（TODO）
func (c *EmbyClient) TestConnection(ctx context.Context) error {
	// TODO
	return nil
}
