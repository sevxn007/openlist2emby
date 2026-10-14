package openlist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

type Client struct {
	httpClient     *http.Client
	transferClient *http.Client
}

var taskKinds = map[string]struct{}{
	"upload":                    {},
	"copy":                      {},
	"move":                      {},
	"offline_download":          {},
	"offline_download_transfer": {},
	"decompress":                {},
	"decompress_upload":         {},
}

type Error struct {
	status  int
	message string
	err     error
}

func (e *Error) Error() string {
	return e.message
}

func (e *Error) Unwrap() error {
	return e.err
}

func (e *Error) StatusCode() int {
	return e.status
}

func (e *Error) PublicMessage() string {
	return e.message
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		transport := http.DefaultTransport
		httpClient = &http.Client{Transport: transport, Timeout: 20 * time.Second}
		return &Client{
			httpClient:     httpClient,
			transferClient: &http.Client{Transport: transport, Timeout: 30 * time.Minute},
		}
	}
	return &Client{httpClient: httpClient, transferClient: httpClient}
}

func (c *Client) TestConnection(ctx context.Context, baseURL, adminKey string) (domain.TestConnectionResult, error) {
	user, err := c.CurrentUser(ctx, baseURL, adminKey)
	if err != nil {
		return domain.TestConnectionResult{}, err
	}
	if user.Role != domain.OpenListAdminRole {
		return domain.TestConnectionResult{}, &Error{status: http.StatusBadRequest, message: "openlist administrator key is required"}
	}
	return domain.TestConnectionResult{
		OpenListBaseURL: baseURL,
		Username:        user.Username,
	}, nil
}

func (c *Client) CurrentUser(ctx context.Context, baseURL, token string) (domain.OpenListUser, error) {
	req, err := c.newRequest(ctx, http.MethodGet, baseURL+"/api/me", nil)
	if err != nil {
		return domain.OpenListUser{}, &Error{status: http.StatusBadGateway, message: "openlist connection failed", err: err}
	}
	req.Header.Set("Authorization", token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.OpenListUser{}, &Error{status: http.StatusBadGateway, message: "openlist connection failed", err: err}
	}
	defer resp.Body.Close()

	var response struct {
		Code    int                         `json:"code"`
		Message string                      `json:"message"`
		Data    openListCurrentUserResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return domain.OpenListUser{}, &Error{status: http.StatusBadGateway, message: "invalid openlist response", err: err}
	}
	if response.Code != http.StatusOK {
		message := strings.TrimSpace(response.Message)
		if message == "" {
			message = "openlist rejected the administrator key"
		}
		return domain.OpenListUser{}, &Error{status: http.StatusBadRequest, message: message}
	}
	return domain.OpenListUser{
		Username: response.Data.Username,
		Role:     response.Data.Role,
	}, nil
}

func (c *Client) ProxyJSON(ctx context.Context, baseURL, path, token string, body []byte) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{status: http.StatusBadGateway, message: "openlist request failed", err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)
	httpClient := c.httpClient
	if path == "/api/fs/batch_rename" {
		// OpenList renames each file synchronously before returning the batch response.
		httpClient = c.transferClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &Error{status: http.StatusBadGateway, message: "openlist request failed", err: err}
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{status: http.StatusBadGateway, message: "openlist request failed", err: err}
	}
	return responseBody, nil
}

func (c *Client) ListTasks(ctx context.Context, baseURL, token, kind string) ([]domain.OpenListTask, error) {
	if _, ok := taskKinds[kind]; !ok {
		return nil, &Error{status: http.StatusBadRequest, message: "unsupported openlist task type"}
	}
	var tasks []domain.OpenListTask
	if err := c.requestJSON(ctx, http.MethodGet, baseURL, "/api/task/"+kind+"/undone", token, nil, &tasks, ""); err != nil {
		return nil, err
	}
	for i := range tasks {
		tasks[i].Kind = kind
		if tasks[i].Progress < 0 {
			tasks[i].Progress = 0
		}
		if tasks[i].Progress > 100 {
			tasks[i].Progress = 100
		}
	}
	return tasks, nil
}

func (c *Client) CancelTask(ctx context.Context, baseURL, token, kind, taskID string) error {
	if _, ok := taskKinds[kind]; !ok {
		return &Error{status: http.StatusBadRequest, message: "unsupported openlist task type"}
	}
	if strings.TrimSpace(taskID) == "" {
		return &Error{status: http.StatusBadRequest, message: "openlist task ID is required"}
	}
	endpoint := "/api/task/" + kind + "/cancel?tid=" + url.QueryEscape(strings.TrimSpace(taskID))
	return c.requestJSON(ctx, http.MethodPost, baseURL, endpoint, token, nil, nil, "")
}

func (c *Client) requestJSON(ctx context.Context, method, baseURL, endpoint, token string, body []byte, result any, userAgent string) error {
	if c == nil || c.httpClient == nil {
		return &Error{status: http.StatusBadGateway, message: "openlist client is not initialized"}
	}
	target := strings.TrimRight(baseURL, "/") + endpoint
	request, err := c.newRequest(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return &Error{status: http.StatusBadGateway, message: "openlist request failed", err: err}
	}
	request.Header.Set("Authorization", token)
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return &Error{status: http.StatusBadGateway, message: "openlist request failed", err: err}
	}
	responseBody, readErr := readLimited(response.Body, maxOpenListBodySize)
	_ = response.Body.Close()
	if readErr != nil {
		return &Error{status: http.StatusBadGateway, message: "openlist request failed", err: readErr}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &Error{status: mapUpstreamStatus(response.StatusCode), message: "openlist request failed"}
	}
	if result == nil {
		var ignored json.RawMessage
		return decodeOpenListEnvelope(responseBody, &ignored)
	}
	return decodeOpenListEnvelope(responseBody, result)
}

const (
	maxAudioTransferSize = int64(2 << 30)
	maxOpenListBodySize  = int64(2 << 20)
)

func (c *Client) DownloadFile(
	ctx context.Context,
	baseURL string,
	path string,
	token string,
	destination io.Writer,
	progress domain.TransferProgress,
) (domain.OpenListFile, error) {
	requestBody, err := json.Marshal(map[string]any{"path": path})
	if err != nil {
		return domain.OpenListFile{}, &Error{status: http.StatusBadGateway, message: "openlist download failed", err: err}
	}
	request, err := c.newRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/fs/get", bytes.NewReader(requestBody))
	if err != nil {
		return domain.OpenListFile{}, &Error{status: http.StatusBadGateway, message: "openlist download failed", err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return domain.OpenListFile{}, &Error{status: http.StatusBadGateway, message: "openlist download failed", err: err}
	}
	responseBody, readErr := readLimited(response.Body, maxOpenListBodySize)
	_ = response.Body.Close()
	if readErr != nil {
		return domain.OpenListFile{}, &Error{status: http.StatusBadGateway, message: "openlist download failed", err: readErr}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.OpenListFile{}, &Error{status: mapUpstreamStatus(response.StatusCode), message: "openlist download failed"}
	}
	var data struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		IsDir  bool   `json:"is_dir"`
		RawURL string `json:"raw_url"`
	}
	if err := decodeOpenListEnvelope(responseBody, &data); err != nil {
		return domain.OpenListFile{}, err
	}
	if data.IsDir || strings.TrimSpace(data.RawURL) == "" {
		return domain.OpenListFile{}, &Error{status: http.StatusUnprocessableEntity, message: "openlist returned no downloadable file"}
	}
	if data.Size > maxAudioTransferSize {
		return domain.OpenListFile{}, &Error{status: http.StatusRequestEntityTooLarge, message: "audio file is too large to process"}
	}
	reportTransferProgress(progress, 0, data.Size)
	downloaderURL, err := resolveDownloadURL(baseURL, data.RawURL)
	if err != nil {
		return domain.OpenListFile{}, err
	}
	downloadRequest, err := c.newRequest(ctx, http.MethodGet, downloaderURL, nil)
	if err != nil {
		return domain.OpenListFile{}, &Error{status: http.StatusBadGateway, message: "openlist download failed", err: err}
	}
	if sameOrigin(baseURL, downloaderURL) {
		downloadRequest.Header.Set("Authorization", token)
	}
	downloadResponse, err := c.transferClient.Do(downloadRequest)
	if err != nil {
		return domain.OpenListFile{}, &Error{status: http.StatusBadGateway, message: "openlist download failed", err: err}
	}
	defer downloadResponse.Body.Close()
	if downloadResponse.StatusCode < 200 || downloadResponse.StatusCode >= 300 {
		return domain.OpenListFile{}, &Error{status: mapUpstreamStatus(downloadResponse.StatusCode), message: "openlist download failed"}
	}
	trackedDestination := destination
	if progress != nil {
		trackedDestination = &transferWriter{
			writer:   destination,
			total:    data.Size,
			progress: progress,
		}
	}
	count, err := io.Copy(trackedDestination, io.LimitReader(downloadResponse.Body, maxAudioTransferSize+1))
	if err != nil {
		return domain.OpenListFile{}, &Error{status: http.StatusBadGateway, message: "openlist download failed", err: err}
	}
	if count > maxAudioTransferSize {
		return domain.OpenListFile{}, &Error{status: http.StatusRequestEntityTooLarge, message: "audio file is too large to process"}
	}
	finalTotal := data.Size
	if finalTotal <= 0 {
		finalTotal = count
	}
	reportTransferProgress(progress, count, finalTotal)
	return domain.OpenListFile{Name: data.Name, Size: count}, nil
}

func (c *Client) UploadFile(
	ctx context.Context,
	baseURL string,
	path string,
	token string,
	source io.Reader,
	size int64,
	contentType string,
	progress domain.TransferProgress,
) error {
	reportTransferProgress(progress, 0, size)
	trackedSource := source
	if progress != nil {
		trackedSource = &transferReader{
			reader:   source,
			total:    size,
			progress: progress,
		}
	}
	request, err := c.newRequest(ctx, http.MethodPut, strings.TrimRight(baseURL, "/")+"/api/fs/put", trackedSource)
	if err != nil {
		return &Error{status: http.StatusBadGateway, message: "openlist upload failed", err: err}
	}
	request.ContentLength = size
	request.Header.Set("Authorization", token)
	request.Header.Set("File-Path", url.PathEscape(path))
	request.Header.Set("Overwrite", "true")
	request.Header.Set("Content-Type", contentType)
	response, err := c.transferClient.Do(request)
	if err != nil {
		return &Error{status: http.StatusBadGateway, message: "openlist upload failed", err: err}
	}
	responseBody, readErr := readLimited(response.Body, maxOpenListBodySize)
	_ = response.Body.Close()
	if readErr != nil {
		return &Error{status: http.StatusBadGateway, message: "openlist upload failed", err: readErr}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &Error{status: mapUpstreamStatus(response.StatusCode), message: "openlist upload failed"}
	}
	if len(strings.TrimSpace(string(responseBody))) == 0 {
		reportTransferProgress(progress, size, size)
		return nil
	}
	if err := ensureOpenListSuccess(responseBody); err != nil {
		return err
	}
	reportTransferProgress(progress, size, size)
	return nil
}

type transferWriter struct {
	writer      io.Writer
	total       int64
	transferred int64
	progress    domain.TransferProgress
}

func (w *transferWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.transferred += int64(written)
	reportTransferProgress(w.progress, w.transferred, w.total)
	return written, err
}

type transferReader struct {
	reader      io.Reader
	total       int64
	transferred int64
	progress    domain.TransferProgress
}

func (r *transferReader) Read(data []byte) (int, error) {
	read, err := r.reader.Read(data)
	r.transferred += int64(read)
	reportTransferProgress(r.progress, r.transferred, r.total)
	return read, err
}

func reportTransferProgress(progress domain.TransferProgress, transferred, total int64) {
	if progress != nil {
		progress(transferred, total)
	}
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("openlist response is too large")
	}
	return data, nil
}

func decodeOpenListEnvelope(body []byte, data any) error {
	var response struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return &Error{status: http.StatusBadGateway, message: "invalid openlist response", err: err}
	}
	if response.Code != http.StatusOK {
		message := strings.TrimSpace(response.Message)
		if message == "" {
			message = "openlist request failed"
		}
		return &Error{status: mapUpstreamStatus(response.Code), message: message}
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(response.Data, data); err != nil {
		return &Error{status: http.StatusBadGateway, message: "invalid openlist response", err: err}
	}
	return nil
}

func ensureOpenListSuccess(body []byte) error {
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return &Error{status: http.StatusBadGateway, message: "invalid openlist response", err: err}
	}
	if response.Code == http.StatusOK {
		return nil
	}
	message := strings.TrimSpace(response.Message)
	if message == "" {
		message = "openlist upload failed"
	}
	return &Error{status: mapUpstreamStatus(response.Code), message: message}
}

func resolveDownloadURL(baseURL, rawURL string) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		return "", &Error{status: http.StatusBadGateway, message: "invalid openlist download URL", err: err}
	}
	target, err := url.Parse(rawURL)
	if err != nil || (target.Scheme != "" && target.Scheme != "http" && target.Scheme != "https") {
		return "", &Error{status: http.StatusBadGateway, message: "invalid openlist download URL"}
	}
	if !target.IsAbs() {
		target = base.ResolveReference(target)
	}
	if target.Scheme != "http" && target.Scheme != "https" || target.Host == "" {
		return "", &Error{status: http.StatusBadGateway, message: "invalid openlist download URL"}
	}
	return target.String(), nil
}

func sameOrigin(left, right string) bool {
	a, errA := url.Parse(left)
	b, errB := url.Parse(right)
	return errA == nil && errB == nil && a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host)
}

func mapUpstreamStatus(status int) int {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
		http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType:
		return status
	default:
		return http.StatusBadGateway
	}
}

func (c *Client) newRequest(ctx context.Context, method, target string, body io.Reader) (*http.Request, error) {
	if strings.TrimSpace(target) == "" {
		return nil, errors.New("openlist URL is empty")
	}
	return http.NewRequestWithContext(ctx, method, target, body)
}

type openListCurrentUserResponse struct {
	Username string `json:"username"`
	Role     int    `json:"role"`
}
