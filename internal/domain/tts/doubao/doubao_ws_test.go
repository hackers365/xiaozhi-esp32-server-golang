package doubao

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestNewDoubaoWSProvider(t *testing.T) {
	config := map[string]interface{}{
		"api_key":      "test-api-key",
		"appid":        "appid",
		"access_token": "access_token",
		"model":        "seed-tts-1.1",
		"voice":        "zh_female_wanwanxiaohe_moon_bigtts",
		"ws_url":       "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream",
	}

	provider := NewDoubaoWSProvider(config)
	if provider.APIKey != "test-api-key" {
		t.Fatalf("APIKey = %q", provider.APIKey)
	}
	if provider.AppID != "appid" {
		t.Fatalf("AppID = %q", provider.AppID)
	}
	if provider.Model != "seed-tts-1.1" {
		t.Fatalf("Model = %q", provider.Model)
	}
	if provider.WSURL != "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream" {
		t.Fatalf("WSURL = %q", provider.WSURL)
	}
}

func TestNewDoubaoWSProviderCompatWsHost(t *testing.T) {
	config := map[string]interface{}{
		"appid":        "appid",
		"access_token": "access_token",
		"voice":        "zh_female_wanwanxiaohe_moon_bigtts",
		"ws_host":      "openspeech.bytedance.com",
	}

	provider := NewDoubaoWSProvider(config)
	want := "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream"
	if provider.WSURL != want {
		t.Fatalf("WSURL = %q, want %q", provider.WSURL, want)
	}
}

func TestNewDoubaoWSProviderNormalizesMissingStreamPath(t *testing.T) {
	config := map[string]interface{}{
		"appid":        "appid",
		"access_token": "access_token",
		"voice":        "zh_female_wanwanxiaohe_moon_bigtts",
		"ws_url":       "wss://openspeech.bytedance.com/api/v3/tts/unidirectional",
	}

	provider := NewDoubaoWSProvider(config)
	want := "wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream"
	if provider.WSURL != want {
		t.Fatalf("WSURL = %q, want %q", provider.WSURL, want)
	}
}

func TestParseDoubaoWSFullServerResponse(t *testing.T) {
	payloadJSON := []byte(`{"code":0,"message":"Success","sequence":1}`)
	frame := []byte{0x11, 0x90, 0x10, 0x00}
	payload := make([]byte, 8+len(payloadJSON))
	binary.BigEndian.PutUint32(payload[0:4], uint32(1))
	binary.BigEndian.PutUint32(payload[4:8], uint32(len(payloadJSON)))
	copy(payload[8:], payloadJSON)
	frame = append(frame, payload...)

	got, isLast, err := parseDoubaoWSMessage(2, frame)
	if err != nil {
		t.Fatalf("parseDoubaoWSMessage error = %v", err)
	}
	if isLast {
		t.Fatal("expected non-final ack")
	}
	if len(got) != 0 {
		t.Fatalf("unexpected audio chunk: %v", got)
	}
}

func TestNewDoubaoWSPayloadUsesV3ReqParams(t *testing.T) {
	payload := newDoubaoWSPayload("测试", "voice-demo", 24000, modelSeedTTS20Standard)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal payload error = %v", err)
	}
	reqParams, ok := got["req_params"].(map[string]any)
	if !ok {
		t.Fatalf("req_params missing: %#v", got)
	}
	if reqParams["text"] != "测试" {
		t.Fatalf("text = %#v", reqParams["text"])
	}
	if reqParams["speaker"] != "voice-demo" {
		t.Fatalf("speaker = %#v", reqParams["speaker"])
	}
	if reqParams["model"] != modelSeedTTS20Standard {
		t.Fatalf("model = %#v", reqParams["model"])
	}
	audio, ok := reqParams["audio_params"].(map[string]any)
	if !ok {
		t.Fatalf("audio_params missing: %#v", reqParams)
	}
	if audio["sample_rate"] != float64(24000) {
		t.Fatalf("sample_rate = %#v", audio["sample_rate"])
	}
	if audio["format"] != defaultDoubaoAudioFmt {
		t.Fatalf("format = %#v", audio["format"])
	}
}

func TestParseDoubaoWSBinaryAudioResponse(t *testing.T) {
	audio := []byte{0x01, 0x02, 0x03, 0x04}
	frame := []byte{0x11, 0xB1, 0x00, 0x00}
	payload := make([]byte, 8+len(audio))
	binary.BigEndian.PutUint32(payload[0:4], uint32(1))
	binary.BigEndian.PutUint32(payload[4:8], uint32(len(audio)))
	copy(payload[8:], audio)
	frame = append(frame, payload...)

	got, isLast, err := parseDoubaoWSMessage(2, frame)
	if err != nil {
		t.Fatalf("parseDoubaoWSMessage error = %v", err)
	}
	if isLast {
		t.Fatal("expected non-final frame")
	}
	if string(got) != string(audio) {
		t.Fatalf("audio = %v, want %v", got, audio)
	}
}

func TestParseDoubaoWSBinaryAudioResponseWithRequestIDEnvelope(t *testing.T) {
	audio := []byte{0x49, 0x44, 0x33}
	requestID := []byte("request-id-123")
	frame := []byte{0x11, 0xB4, 0x00, 0x00}
	payload := make([]byte, 0, 12+len(requestID)+len(audio))
	first := make([]byte, 4)
	second := make([]byte, 4)
	third := make([]byte, 4)
	binary.BigEndian.PutUint32(first, uint32(352))
	binary.BigEndian.PutUint32(second, uint32(len(requestID)))
	binary.BigEndian.PutUint32(third, uint32(len(audio)))
	payload = append(payload, first...)
	payload = append(payload, second...)
	payload = append(payload, requestID...)
	payload = append(payload, third...)
	payload = append(payload, audio...)
	frame = append(frame, payload...)

	got, isLast, err := parseDoubaoWSMessage(2, frame)
	if err != nil {
		t.Fatalf("parseDoubaoWSMessage error = %v", err)
	}
	if isLast {
		t.Fatal("expected non-final frame")
	}
	if string(got) != string(audio) {
		t.Fatalf("audio = %v, want %v", got, audio)
	}
}

func TestParseDoubaoWSFullServerResponseFinalMarker(t *testing.T) {
	requestID := []byte("request-id-123")
	frame := []byte{0x11, 0x94, 0x00, 0x00}
	payload := make([]byte, 0, 12+len(requestID)+2)
	first := make([]byte, 4)
	second := make([]byte, 4)
	third := make([]byte, 4)
	binary.BigEndian.PutUint32(first, uint32(152))
	binary.BigEndian.PutUint32(second, uint32(len(requestID)))
	binary.BigEndian.PutUint32(third, uint32(2))
	payload = append(payload, first...)
	payload = append(payload, second...)
	payload = append(payload, requestID...)
	payload = append(payload, third...)
	payload = append(payload, []byte("{}")...)
	frame = append(frame, payload...)

	got, isLast, err := parseDoubaoWSMessage(2, frame)
	if err != nil {
		t.Fatalf("parseDoubaoWSMessage error = %v", err)
	}
	if !isLast {
		t.Fatal("expected final frame")
	}
	if len(got) != 0 {
		t.Fatalf("audio = %v", got)
	}
}

func TestParseDoubaoWSFullServerResponseSessionFinishedWithUsage(t *testing.T) {
	requestID := []byte("request-id-123")
	body := []byte(`{"usage":{"text_words":5}}`)
	frame := []byte{0x11, 0x94, 0x10, 0x00}
	payload := make([]byte, 0, 12+len(requestID)+len(body))
	first := make([]byte, 4)
	second := make([]byte, 4)
	third := make([]byte, 4)
	binary.BigEndian.PutUint32(first, uint32(doubaoWSEventSessionFinished))
	binary.BigEndian.PutUint32(second, uint32(len(requestID)))
	binary.BigEndian.PutUint32(third, uint32(len(body)))
	payload = append(payload, first...)
	payload = append(payload, second...)
	payload = append(payload, requestID...)
	payload = append(payload, third...)
	payload = append(payload, body...)
	frame = append(frame, payload...)

	got, isLast, err := parseDoubaoWSMessage(2, frame)
	if err != nil {
		t.Fatalf("parseDoubaoWSMessage error = %v", err)
	}
	if !isLast {
		t.Fatal("expected SessionFinished with usage payload to mark final frame")
	}
	if len(got) != 0 {
		t.Fatalf("audio = %v", got)
	}
}

func TestParseDoubaoWSFullServerResponseSessionFailed(t *testing.T) {
	requestID := []byte("request-id-123")
	body := []byte(`{"error":"session failed"}`)
	frame := []byte{0x11, 0x94, 0x10, 0x00}
	payload := make([]byte, 0, 12+len(requestID)+len(body))
	first := make([]byte, 4)
	second := make([]byte, 4)
	third := make([]byte, 4)
	binary.BigEndian.PutUint32(first, uint32(doubaoWSEventSessionFailed))
	binary.BigEndian.PutUint32(second, uint32(len(requestID)))
	binary.BigEndian.PutUint32(third, uint32(len(body)))
	payload = append(payload, first...)
	payload = append(payload, second...)
	payload = append(payload, requestID...)
	payload = append(payload, third...)
	payload = append(payload, body...)
	frame = append(frame, payload...)

	if _, _, err := parseDoubaoWSMessage(2, frame); err == nil {
		t.Fatal("expected SessionFailed event to return error")
	}
}

func TestParseDoubaoWSJSONMessage(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("mp3"))
	payload := []byte(`{"code":0,"message":"","data":"` + data + `","sequence":-1}`)

	got, isLast, err := parseDoubaoWSMessage(1, payload)
	if err != nil {
		t.Fatalf("parseDoubaoWSMessage error = %v", err)
	}
	if !isLast {
		t.Fatal("expected final frame")
	}
	if string(got) != "mp3" {
		t.Fatalf("audio = %q", got)
	}
}
