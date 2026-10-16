package actordb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// EmbyClient Emby 客户端
// 注意：UserID 字段可以填「用户名」或「UUID」，构造后会自动识别。
type EmbyClient struct {
	BaseURL string
	APIKey  string
	UserID  string
	http    *http.Client
}

func NewEmbyClient(baseURL, apiKey, userID string) *EmbyClient {
	return &EmbyClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		UserID:  userID,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// ============ 媒体库 ============

type EmbyLibrary struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
	Type string `json:"CollectionType"`
}

// ListLibraries 列出所有媒体库
func (c *EmbyClient) ListLibraries(ctx context.Context) ([]EmbyLibrary, error) {
	userID, err := c.ResolveUserID(ctx, c.UserID)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/emby/Users/%s/Views?api_key=%s",
		c.BaseURL, userID, c.APIKey)
	var resp struct {
		Items []EmbyLibrary `json:"Items"`
	}
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// ============ 演员列表 ============

type EmbyPerson struct {
	ID       string `json:"Id"`
	Name     string `json:"Name"`
	Type     string `json:"Type"`
	Role     string `json:"Role,omitempty"`
	ImageTag string `json:"PrimaryImageTag,omitempty"`
}

// ListPersonsInLibrary 拉取某媒体库下的所有演员（去重）
func (c *EmbyClient) ListPersonsInLibrary(ctx context.Context, libraryID string) ([]EmbyPerson, error) {
	userID, err := c.ResolveUserID(ctx, c.UserID)
	if err != nil {
		return nil, err
	}

	const pageSize = 500
	startIndex := 0
	seen := make(map[string]struct{})
	out := make([]EmbyPerson, 0)

	for {
		u := fmt.Sprintf(
			"%s/emby/Users/%s/Items?ParentId=%s&Recursive=true&Fields=People&StartIndex=%d&Limit=%d&api_key=%s",
			c.BaseURL, userID, libraryID, startIndex, pageSize, c.APIKey,
		)
		var resp struct {
			Items []struct {
				People []EmbyPerson `json:"People"`
			} `json:"Items"`
			TotalRecordCount int `json:"TotalRecordCount"`
		}
		if err := c.getJSON(ctx, u, &resp); err != nil {
			return nil, err
		}
		if len(resp.Items) == 0 {
			break
		}
		for _, item := range resp.Items {
			for _, p := range item.People {
				if p.Type != "Person" {
					continue
				}
				if _, ok := seen[p.ID]; ok {
					continue
				}
				seen[p.ID] = struct{}{}
				out = append(out, p)
			}
		}
		if startIndex+len(resp.Items) >= resp.TotalRecordCount {
			break
		}
		startIndex += pageSize
	}
	return out, nil
}

// ============ 演员详情 ============

type EmbyPersonDetail struct {
	ID                 string   `json:"Id"`
	Name               string   `json:"Name"`
	Biography          string   `json:"Biography"`
	Birthday           string   `json:"Birthday"`
	PlaceOfBirth       string   `json:"PlaceOfBirth"`
	KnownForDepartment string   `json:"KnownForDepartment"`
	Tags               []string `json:"Tags"`
	Taglines           []string `json:"Taglines"`
	ImageURL           string   `json:"imageUrl"`
}

// GetPersonDetail 获取演员详情（Lux API）
func (c *EmbyClient) GetPersonDetail(ctx context.Context, personID string) (*EmbyPersonDetail, error) {
	u := fmt.Sprintf("%s/api/v1/people/%s?api_key=%s",
		c.BaseURL, personID, c.APIKey)
	var resp EmbyPersonDetail
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	// 兜底：用 Emby 原生端点拼头像地址
	if resp.ImageURL == "" {
		resp.ImageURL = fmt.Sprintf("%s/emby/Items/%s/Images/Primary?api_key=%s",
			c.BaseURL, personID, c.APIKey)
	}
	return &resp, nil
}

// ============ 搜索 ============

// SearchPersons 按名字搜索演员
func (c *EmbyClient) SearchPersons(ctx context.Context, keyword string) ([]EmbyPerson, error) {
	u := fmt.Sprintf("%s/api/v1/people?q=%s&api_key=%s",
		c.BaseURL, keyword, c.APIKey)
	var resp struct {
		Items []EmbyPerson `json:"items"`
	}
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// ============ 头像代理 ============

// FetchImage 拉取演员头像二进制（供后端代理给前端）
func (c *EmbyClient) FetchImage(ctx context.Context, personID string) ([]byte, string, error) {
	u := fmt.Sprintf("%s/api/v1/people/%s/image?api_key=%s",
		c.BaseURL, personID, c.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("emby 返回 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// ============ 用户解析 ============

// EmbyUser Emby 用户
type EmbyUser struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

// ListUsers 拉取所有 Emby 用户
func (c *EmbyClient) ListUsers(ctx context.Context) ([]EmbyUser, error) {
	u := fmt.Sprintf("%s/emby/Users?api_key=%s", c.BaseURL, c.APIKey)
	var users []EmbyUser
	if err := c.getJSON(ctx, u, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// ResolveUserID 把用户名解析成 UserID。
// 如果传入的字符串本身就是 UUID（36 位带 4 个 -），直接返回。
func (c *EmbyClient) ResolveUserID(ctx context.Context, username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("用户名或 ID 为空")
	}
	// 已经是 UUID 格式直接返回
	if len(username) == 36 && strings.Count(username, "-") == 4 {
		return username, nil
	}
	users, err := c.ListUsers(ctx)
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.Name == username {
			return u.ID, nil
		}
	}
	for _, u := range users {
		if strings.EqualFold(u.Name, username) {
			return u.ID, nil
		}
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Name)
	}
	return "", fmt.Errorf("用户 %q 不存在，现有用户：%s", username, strings.Join(names, ", "))
}

// ============ 工具 ============

func (c *EmbyClient) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("emby 返回 %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// TestConnection 测试连接
func (c *EmbyClient) TestConnection(ctx context.Context) error {
	u := fmt.Sprintf("%s/emby/System/Info?api_key=%s", c.BaseURL, c.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// PushPerson 推送演员到 Emby（保留原接口，后续填真实逻辑）
func (c *EmbyClient) PushPerson(ctx context.Context, p *ActorProfile) error {
	// TODO: PATCH /api/v1/people/{id} 更新元数据 + POST /emby/Items/{id}/Images/Primary 上传头像
	return nil
}
