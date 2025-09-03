package pufferpanel

import (
    "context"
    "encoding/json"
    "net/http"
    "net/url"
    "strings"
)

// valueWrapper mirrors the typical { value: any } pattern PufferPanel returns.
type ValueWrapper struct {
    Value any `json:"value"`
}

// ServerData represents current values for template variables.
type ServerData struct {
    Data map[string]ValueWrapper `json:"data"`
}

// GetServerData fetches the current data values for a server.
func GetServerData(ctx context.Context, id string) (*ServerData, error) {
    creds, err := getCreds()
    if err != nil { return nil, err }
    u, err := url.Parse(creds.BaseURL)
    if err != nil { return nil, err }
    u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + id + "/data"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
    if err != nil { return nil, err }
    client := newClient(u)
    status, body, err := doAuthRequest(ctx, client, req)
    if err != nil { return nil, err }
    if status < 200 || status >= 300 {
        return nil, parseError(status, body)
    }
    var d ServerData
    if err := json.Unmarshal(body, &d); err != nil { return nil, err }
    if d.Data == nil { d.Data = map[string]ValueWrapper{} }
    return &d, nil
}
