package pufferpanel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// FileEntry represents a file or directory returned by PufferPanel's file listing API.
type FileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

// listFiles retrieves the contents of the given path for a server.
func listFiles(ctx context.Context, serverID, path string) ([]FileEntry, error) {
	creds, err := getCreds()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + serverID + "/files/list"
	q := u.Query()
	q.Set("path", path)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := newClient(u)
	status, body, err := doAuthRequest(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, parseError(status, body)
	}
	var files []FileEntry
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// FetchFile retrieves raw bytes for the given path on a server.
func FetchFile(ctx context.Context, serverID, path string) ([]byte, error) {
	creds, err := getCreds()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + serverID + "/files/contents"
	q := u.Query()
	q.Set("path", path)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := newClient(u)
	status, body, err := doAuthRequest(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, parseError(status, body)
	}
	return body, nil
}

// ListJarFiles returns .jar files under mods/ or plugins/ for the server.
func ListJarFiles(ctx context.Context, serverID string) ([]string, error) {
	files, err := listFiles(ctx, serverID, "mods")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			files, err = listFiles(ctx, serverID, "plugins")
		}
		if err != nil {
			return nil, err
		}
	}
	jars := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir {
			continue
		}
		if strings.HasSuffix(strings.ToLower(f.Name), ".jar") {
			jars = append(jars, f.Name)
		}
	}
	sort.Strings(jars)
	return jars, nil
}

// ListPath retrieves file or directory entries under the given path.
func ListPath(ctx context.Context, serverID, path string) ([]FileEntry, error) {
	creds, err := getCreds()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + serverID + "/file/" + url.PathEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := newClient(u)
	status, body, err := doAuthRequest(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, parseError(status, body)
	}
	var files []FileEntry
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// PutFile uploads file contents to the given path.
func PutFile(ctx context.Context, serverID, path string, data []byte) error {
	creds, err := getCreds()
	if err != nil {
		return err
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + serverID + "/file/" + url.PathEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	client := newClient(u)
	status, body, err := doAuthRequest(ctx, client, req)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return parseError(status, body)
	}
	return nil
}

// DeleteFile removes the file at the given path.
func DeleteFile(ctx context.Context, serverID, path string) error {
	creds, err := getCreds()
	if err != nil {
		return err
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + serverID + "/file/" + url.PathEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	client := newClient(u)
	status, body, err := doAuthRequest(ctx, client, req)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return parseError(status, body)
	}
	return nil
}
