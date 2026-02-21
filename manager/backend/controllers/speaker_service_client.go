package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"xiaozhi/manager/backend/config"
)

const (
	speakerServiceModeHTTP  = "http"
	speakerServiceModeEmbed = "embed"
)

type speakerServiceClient interface {
	Register(ctx context.Context, req speakerRegisterRequest) error
	Verify(ctx context.Context, req speakerVerifyRequest) (*VerifyResult, error)
	Delete(ctx context.Context, req speakerDeleteRequest) error
	Mode() string
}

type speakerRegisterRequest struct {
	SpeakerID   string
	SpeakerName string
	UUID        string
	AgentID     uint
	UserID      string
	AudioData   []byte
	FileName    string
}

type speakerVerifyRequest struct {
	SpeakerID string
	AgentID   uint
	UserID    string
	AudioData []byte
	FileName  string
}

type speakerDeleteRequest struct {
	SpeakerID string
	AgentID   uint
	UserID    string
	UUID      string
}

type httpSpeakerServiceClient struct {
	serviceURL string
	httpClient *http.Client
}

func newSpeakerServiceClient(cfg config.SpeakerServiceConfig, httpClient *http.Client) (speakerServiceClient, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = speakerServiceModeHTTP
	}

	switch mode {
	case speakerServiceModeEmbed:
		return newEmbeddedSpeakerServiceClient(cfg)
	case speakerServiceModeHTTP:
		baseURL := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
		if baseURL == "" {
			return nil, fmt.Errorf("speaker_service.url 不能为空")
		}
		return &httpSpeakerServiceClient{
			serviceURL: baseURL,
			httpClient: httpClient,
		}, nil
	default:
		return nil, fmt.Errorf("不支持的 speaker_service.mode: %s，仅支持 http/embed", cfg.Mode)
	}
}

func (c *httpSpeakerServiceClient) Mode() string {
	return speakerServiceModeHTTP
}

func (c *httpSpeakerServiceClient) Register(ctx context.Context, req speakerRegisterRequest) error {
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	_ = writer.WriteField("speaker_id", req.SpeakerID)
	_ = writer.WriteField("speaker_name", req.SpeakerName)
	_ = writer.WriteField("uuid", req.UUID)
	_ = writer.WriteField("agent_id", fmt.Sprintf("%d", req.AgentID))
	_ = writer.WriteField("uid", req.UserID)

	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "audio.wav"
	}
	part, err := writer.CreateFormFile("audio", fileName)
	if err != nil {
		_ = writer.Close()
		return fmt.Errorf("创建文件字段失败: %w", err)
	}
	if _, err := part.Write(req.AudioData); err != nil {
		_ = writer.Close()
		return fmt.Errorf("写入音频数据失败: %w", err)
	}
	_ = writer.Close()

	apiURL := fmt.Sprintf("%s/api/v1/speaker/register", c.serviceURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &requestBody)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("X-User-ID", req.UserID)
	httpReq.Header.Set("X-Agent-ID", fmt.Sprintf("%d", req.AgentID))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("asr_server 返回错误: %s", string(body))
	}
	return nil
}

func (c *httpSpeakerServiceClient) Verify(ctx context.Context, req speakerVerifyRequest) (*VerifyResult, error) {
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "audio.wav"
	}
	part, err := writer.CreateFormFile("audio", fileName)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("创建文件字段失败: %w", err)
	}
	if _, err := part.Write(req.AudioData); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("写入音频数据失败: %w", err)
	}
	_ = writer.Close()

	apiURL := fmt.Sprintf("%s/api/v1/speaker/verify/%s", c.serviceURL, url.PathEscape(req.SpeakerID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("X-User-ID", req.UserID)
	httpReq.Header.Set("X-Agent-ID", fmt.Sprintf("%d", req.AgentID))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asr_server 返回错误 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	var result VerifyResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return &result, nil
}

func (c *httpSpeakerServiceClient) Delete(ctx context.Context, req speakerDeleteRequest) error {
	apiURL := fmt.Sprintf("%s/api/v1/speaker/%s", c.serviceURL, url.PathEscape(req.SpeakerID))
	if req.UUID != "" {
		apiURL += "?uuid=" + url.QueryEscape(req.UUID)
	}
	separator := "?"
	if strings.Contains(apiURL, "?") {
		separator = "&"
	}
	apiURL += fmt.Sprintf("%sagent_id=%d", separator, req.AgentID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("X-User-ID", req.UserID)
	httpReq.Header.Set("X-Agent-ID", fmt.Sprintf("%d", req.AgentID))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("asr_server 返回错误: %s", string(body))
	}
	return nil
}
