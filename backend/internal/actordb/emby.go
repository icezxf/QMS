package actordb

type EmbyClient struct {
	BaseURL string
	APIKey  string
	User    string
}

func NewEmbyClient(baseURL, apiKey, user string) *EmbyClient {
	return &EmbyClient{BaseURL: baseURL, APIKey: apiKey, User: user}
}
