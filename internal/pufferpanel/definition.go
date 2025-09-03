package pufferpanel

import (
    "context"
    "encoding/json"
    "net/http"
    "net/url"
    "strings"
)

// Variable describes a template variable in a server definition.
type Variable struct {
    Display string   `json:"display"`
    Desc    string   `json:"desc"`
    Options []string `json:"options"`
}

// ServerDefinition is the template definition for a server, including variable metadata.
type ServerDefinition struct {
    Data map[string]Variable `json:"data"`
}

// GetServerDefinition fetches the template definition for a server.
func GetServerDefinition(ctx context.Context, id string) (*ServerDefinition, error) {
    creds, err := getCreds()
    if err != nil { return nil, err }
    u, err := url.Parse(creds.BaseURL)
    if err != nil { return nil, err }
    u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + id + "/definition"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
    if err != nil { return nil, err }
    client := newClient(u)
    status, body, err := doAuthRequest(ctx, client, req)
    if err != nil { return nil, err }
    if status < 200 || status >= 300 {
        return nil, parseError(status, body)
    }
    var def ServerDefinition
    if err := json.Unmarshal(body, &def); err != nil { return nil, err }
    if def.Data == nil { def.Data = map[string]Variable{} }
    return &def, nil
}

