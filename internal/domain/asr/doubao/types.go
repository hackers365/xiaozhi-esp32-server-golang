package doubao

// DoubaoV2Config 豆包ASR配置结构体
type DoubaoV2Config struct {
	AppID         string // 应用ID
	AccessToken   string // 访问令牌
	WsURL         string // WebSocket URL
	ResourceID    string // 资源ID
	ModelName     string // 模型名称
	EndWindowSize int    // 结束窗口大小
	EnablePunc    bool   // 是否启用标点符号
	EnableITN     bool   // 是否启用ITN
	EnableDDC     bool   // 是否启用DDC
	ChunkDuration int    // 分块时长(毫秒)
	Timeout       int    // 超时时间(秒)
}

// DefaultConfig 默认配置
var DefaultConfig = DoubaoV2Config{
	WsURL:         "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream",
	ResourceID:    "volc.bigasr.sauc.duration",
	ModelName:     "bigmodel",
	EndWindowSize: 800,
	EnablePunc:    true,
	EnableITN:     true,
	EnableDDC:     false,
	ChunkDuration: 200,
	Timeout:       30,
}

// DoubaoV2Request 豆包ASR请求结构体
type DoubaoV2Request struct {
	User struct {
		UID string `json:"uid"`
	} `json:"user"`
	Audio struct {
		Format   string `json:"format"`
		Rate     int    `json:"rate"`
		Bits     int    `json:"bits"`
		Channel  int    `json:"channel"`
		Language string `json:"language"`
	} `json:"audio"`
	Request struct {
		ModelName      string `json:"model_name"`
		EndWindowSize  int    `json:"end_window_size"`
		EnablePunc     bool   `json:"enable_punc"`
		EnableITN      bool   `json:"enable_itn"`
		EnableDDC      bool   `json:"enable_ddc"`
		ResultType     string `json:"result_type"`
		ShowUtterances bool   `json:"show_utterances"`
	} `json:"request"`
}

// DoubaoV2Response 豆包ASR响应结构体
type DoubaoV2Response struct {
	Code   int `json:"code"`
	Result struct {
		Text string `json:"text"`
	} `json:"result,omitempty"`
	Error string `json:"error,omitempty"`
}
