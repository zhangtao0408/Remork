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
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) Client {
	return Client{base: base, http: http.DefaultClient}
}

func (c Client) Manifest(root, path string) (api.ManifestResponse, error) {
	u, _ := url.Parse(c.base + "/manifest")
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	q.Set("recursive", "true")
	u.RawQuery = q.Encode()
	resp, err := c.http.Get(u.String())
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
	resp, err := c.http.Post(u.String(), "application/json", bytes.NewReader(data))
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
