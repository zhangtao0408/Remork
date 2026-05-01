package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"remork/internal/api"
	"remork/internal/apply"
	execx "remork/internal/exec"
	"remork/internal/limits"
	"remork/internal/ops"
)

type Client struct {
	base     string
	http     *http.Client
	clientID string
	token    string
}

type Options struct {
	BaseURL  string
	ClientID string
	Token    string
	HTTP     *http.Client
	NoProxy  bool
}

func New(base string) Client {
	return NewWithOptions(Options{BaseURL: base})
}

func NewWithClientID(base, clientID string) Client {
	return NewWithOptions(Options{BaseURL: base, ClientID: clientID})
}

func NewWithOptions(opts Options) Client {
	httpClient := opts.HTTP
	if httpClient == nil {
		httpClient = NewHTTPClient(opts.NoProxy)
	}
	return Client{base: opts.BaseURL, http: httpClient, clientID: opts.ClientID, token: opts.Token}
}

func NewHTTPClient(noProxy bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = limits.DefaultHTTPTimeout
	if noProxy {
		transport.Proxy = nil
	}
	return &http.Client{Transport: transport, Timeout: limits.DefaultHTTPTimeout}
}

func (c Client) endpoint(path string) string {
	return strings.TrimRight(c.base, "/") + path
}

func (c Client) Status() (api.StatusResponse, error) {
	return c.StatusContext(context.Background())
}

func (c Client) StatusContext(ctx context.Context) (api.StatusResponse, error) {
	ctx = contextOrBackground(ctx)
	u, _ := url.Parse(c.endpoint("/status"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return api.StatusResponse{}, err
	}
	c.addHeaders(req)
	resp, err := c.do(req)
	if err != nil {
		return api.StatusResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readErrorBody(resp.Body)
		return api.StatusResponse{}, &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	var out api.StatusResponse
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func (c Client) Manifest(root, path string) (api.ManifestResponse, error) {
	return c.ManifestContext(context.Background(), root, path)
}

func (c Client) ManifestContext(ctx context.Context, root, path string) (api.ManifestResponse, error) {
	ctx = contextOrBackground(ctx)
	u, _ := url.Parse(c.endpoint("/manifest"))
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	q.Set("recursive", "true")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return api.ManifestResponse{}, err
	}
	c.addHeaders(req)
	resp, err := c.do(req)
	if err != nil {
		return api.ManifestResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readErrorBody(resp.Body)
		return api.ManifestResponse{}, &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	var out api.ManifestResponse
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func (c Client) Download(root, path string) ([]byte, error) {
	return c.DownloadContext(context.Background(), root, path)
}

func (c Client) DownloadRange(root, path string, start, end int64) ([]byte, error) {
	return c.DownloadRangeContext(context.Background(), root, path, start, end)
}

func (c Client) DownloadContext(ctx context.Context, root, path string) ([]byte, error) {
	return c.download(ctx, root, path, "")
}

func (c Client) DownloadRangeContext(ctx context.Context, root, path string, start, end int64) ([]byte, error) {
	return c.download(ctx, root, path, fmt.Sprintf("bytes=%d-%d", start, end))
}

func (c Client) Apply(root string, cs apply.Changeset) (apply.Result, error) {
	return c.ApplyContext(context.Background(), root, cs)
}

func (c Client) ApplyContext(ctx context.Context, root string, cs apply.Changeset) (apply.Result, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx, limits.DefaultApplyTimeout)
	defer cancel()
	u, _ := url.Parse(c.endpoint("/apply"))
	q := u.Query()
	q.Set("root", root)
	u.RawQuery = q.Encode()
	data, err := json.Marshal(cs)
	if err != nil {
		return apply.Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return apply.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addHeaders(req)
	resp, err := c.doWithoutWholeRequestTimeout(req)
	if err != nil {
		return apply.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readBodyBytes(resp.Body, limits.MaxApplyResultBodyBytes)
		errorBody := boundedBodyString(body, limits.MaxErrorBodyBytes)
		var result apply.Result
		if err := json.Unmarshal(body, &result); err != nil {
			return apply.Result{}, &HTTPError{StatusCode: resp.StatusCode, Body: errorBody}
		}
		return result, &HTTPError{StatusCode: resp.StatusCode, Body: errorBody}
	}
	var result apply.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return apply.Result{}, err
	}
	return result, nil
}

func (c Client) Exec(root, cwd string, command []string, timeoutMillis int64) (execx.Result, error) {
	return c.ExecContext(context.Background(), root, cwd, command, timeoutMillis)
}

func (c Client) ExecContext(ctx context.Context, root, cwd string, command []string, timeoutMillis int64) (execx.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	u, _ := url.Parse(c.endpoint("/exec"))
	reqBody := api.ExecRequest{Root: root, Cwd: cwd, Command: command, TimeoutMillis: timeoutMillis}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return execx.Result{}, err
	}
	timeout := limits.DefaultExecTimeout
	if timeoutMillis > 0 {
		timeout = time.Duration(timeoutMillis) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+limits.OperationTimeoutSlack)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return execx.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addHeaders(req)
	resp, err := c.doWithoutWholeRequestTimeout(req)
	if err != nil {
		return execx.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readErrorBody(resp.Body)
		return execx.Result{}, &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	var result execx.Result
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (c Client) Operations(root string, limit int) ([]ops.Entry, error) {
	return c.OperationsContext(context.Background(), root, limit)
}

func (c Client) OperationsContext(ctx context.Context, root string, limit int) ([]ops.Entry, error) {
	ctx = contextOrBackground(ctx)
	u, _ := url.Parse(c.endpoint("/operations"))
	q := u.Query()
	if root != "" {
		q.Set("root", root)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.addHeaders(req)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readErrorBody(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	var out struct {
		Entries []ops.Entry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

func (c Client) ShellSessions(ctx context.Context, root string) ([]api.ShellSessionInfo, error) {
	ctx = contextOrBackground(ctx)
	u, _ := url.Parse(c.endpoint("/shell/sessions"))
	q := u.Query()
	q.Set("root", root)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.addHeaders(req)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readErrorBody(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	var out struct {
		Sessions []api.ShellSessionInfo `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

func (c Client) KillShellSession(ctx context.Context, root, id string) error {
	ctx = contextOrBackground(ctx)
	u, _ := url.Parse(c.endpoint("/shell/sessions"))
	q := u.Query()
	q.Set("root", root)
	q.Set("id", id)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	c.addHeaders(req)
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readErrorBody(resp.Body)
		return &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	return nil
}

func (c Client) download(ctx context.Context, root, path, byteRange string) ([]byte, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx, limits.DefaultTransferTimeout)
	defer cancel()
	u, _ := url.Parse(c.endpoint("/download"))
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	c.addHeaders(req)
	resp, err := c.doWithoutWholeRequestTimeout(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body := readErrorBody(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	return io.ReadAll(resp.Body)
}

func readErrorBody(r io.Reader) string {
	return string(readErrorBodyBytes(r))
}

func readErrorBodyBytes(r io.Reader) []byte {
	return readBodyBytes(r, limits.MaxErrorBodyBytes)
}

func readBodyBytes(r io.Reader, maxBytes int64) []byte {
	data, _ := io.ReadAll(io.LimitReader(r, maxBytes))
	return data
}

func boundedBodyString(data []byte, maxBytes int64) string {
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	return string(data)
}

func contextWithDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = contextOrBackground(ctx)
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (c Client) do(req *http.Request) (*http.Response, error) {
	if c.http == nil {
		return NewHTTPClient(false).Do(req)
	}
	return c.http.Do(req)
}

func (c Client) doWithoutWholeRequestTimeout(req *http.Request) (*http.Response, error) {
	httpClient := c.http
	if httpClient == nil {
		httpClient = NewHTTPClient(false)
	}
	clientWithoutWholeRequestTimeout := *httpClient
	clientWithoutWholeRequestTimeout.Timeout = 0
	var clonedTransport *http.Transport
	if transport, ok := httpClient.Transport.(*http.Transport); ok {
		longOperationTransport := transport.Clone()
		longOperationTransport.ResponseHeaderTimeout = 0
		clientWithoutWholeRequestTimeout.Transport = longOperationTransport
		clonedTransport = longOperationTransport
	}
	resp, err := clientWithoutWholeRequestTimeout.Do(req)
	if clonedTransport == nil {
		return resp, err
	}
	if err != nil {
		clonedTransport.CloseIdleConnections()
		return resp, err
	}
	if resp != nil && resp.Body != nil {
		resp.Body = closeIdleConnectionsOnClose{
			ReadCloser: resp.Body,
			transport:  clonedTransport,
		}
	}
	return resp, nil
}

type closeIdleConnectionsOnClose struct {
	io.ReadCloser
	transport *http.Transport
}

func (b closeIdleConnectionsOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.transport.CloseIdleConnections()
	return err
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
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
