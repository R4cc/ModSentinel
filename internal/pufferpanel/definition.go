package pufferpanel

import (
    "context"
    "encoding/json"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"

    "modsentinel/internal/telemetry"
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
    start := time.Now()
    status, body, err := doAuthRequest(ctx, client, req)
    telemetry.Event("pufferpanel_request", map[string]string{
        "resource":    "pufferpanel.definition",
        "status":      map[bool]string{true: "ok", false: "error"}[err == nil && status >= 200 && status < 300],
        "duration_ms": strconv.FormatInt(time.Since(start).Milliseconds(), 10),
    })
    if err != nil { return nil, err }
    if status < 200 || status >= 300 {
        return nil, parseError(status, body)
    }
    var def ServerDefinition
    if err := json.Unmarshal(body, &def); err != nil { return nil, err }
    if def.Data == nil { def.Data = map[string]Variable{} }
    return &def, nil
}

// GetServerDefinitionRaw fetches the full template definition JSON as a generic map.
func GetServerDefinitionRaw(ctx context.Context, id string) (map[string]any, error) {
    creds, err := getCreds()
    if err != nil { return nil, err }
    u, err := url.Parse(creds.BaseURL)
    if err != nil { return nil, err }
    u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + id + "/definition"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
    if err != nil { return nil, err }
    client := newClient(u)
    start := time.Now()
    status, body, err := doAuthRequest(ctx, client, req)
    telemetry.Event("pufferpanel_request", map[string]string{
        "resource":    "pufferpanel.definition",
        "status":      map[bool]string{true: "ok", false: "error"}[err == nil && status >= 200 && status < 300],
        "duration_ms": strconv.FormatInt(time.Since(start).Milliseconds(), 10),
    })
    if err != nil { return nil, err }
    if status < 200 || status >= 300 {
        return nil, parseError(status, body)
    }
    var raw map[string]any
    if err := json.Unmarshal(body, &raw); err != nil { return nil, err }
    if raw == nil { raw = map[string]any{} }
    return raw, nil
}
