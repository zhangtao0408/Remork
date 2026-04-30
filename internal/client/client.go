package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"remork/internal/api"
	"remork/internal/apply"
	execx "remork/internal/exec"
	"remork/internal/ops"
)

type Client struct {
	base     string
	http     *http.Client
	clientID string
}

func New(base string) Client {
	return Client{base: base, http: http.DefaultClient}
}

func NewWithClientID(base, clientID string) Client {
	return Client{base: base, http: http.DefaultClient, clientID: clientID}
}

func (c Client) Manifest(root, path string) (api.ManifestResponse, error) {
	u, _ := url.Parse(c.base + "/manifest")
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	q.Set("recursive", "true")
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return api.ManifestResponse{}, err
	}
	c.addHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return api.ManifestResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return api.ManifestResponse{}, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var out api.ManifestResponse
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func (c Client) Download(root, path string) ([]byte, error) {
	return c.download(root, path, "")
}

func (c Client) DownloadRange(root, path string, start, end int64) ([]byte, error) {
	return c.download(root, path, fmt.Sprintf("bytes=%d-%d", start, end))
}

func (c Client) Apply(root string, cs apply.Changeset) (apply.Result, error) {
	u, _ := url.Parse(c.base + "/apply")
	q := u.Query()
	q.Set("root", root)
	u.RawQuery = q.Encode()
	data, err := json.Marshal(cs)
	if err != nil {
		return apply.Result{}, err
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return apply.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return apply.Result{}, err
	}
	defer resp.Body.Close()
	var result apply.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return apply.Result{}, err
	}
	if resp.StatusCode >= 300 {
		return result, &HTTPError{StatusCode: resp.StatusCode, Body: "apply failed"}
	}
	return result, nil
}

func (c Client) Exec(root, cwd string, command []string, timeoutMillis int64) (execx.Result, error) {
	u, _ := url.Parse(c.base + "/exec")
	reqBody := api.ExecRequest{Root: root, Cwd: cwd, Command: command, TimeoutMillis: timeoutMillis}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return execx.Result{}, err
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return execx.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return execx.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return execx.Result{}, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var result execx.Result
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (c Client) Operations(root string, limit int) ([]ops.Entry, error) {
	u, _ := url.Parse(c.base + "/operations")
	q := u.Query()
	if root != "" {
		q.Set("root", root)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.addHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var out struct {
		Entries []ops.Entry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

func (c Client) download(root, path, byteRange string) ([]byte, error) {
	u, _ := url.Parse(c.base + "/download")
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	c.addHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return io.ReadAll(resp.Body)
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return e.Body
}

func (c Client) addHeaders(req *http.Request) {
	if c.clientID != "" {
		req.Header.Set(api.HeaderClientID, c.clientID)
	}
}
