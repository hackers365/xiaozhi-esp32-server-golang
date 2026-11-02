package mimo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultURL = "https://api.xiaomimimo.com/v1/chat/completions"
// Xiaomi documents a 10 MB limit on Base64-encoded input; use decimal MB.
const MaxBase64Bytes = 10_000_000
const MaxASRSamples = (MaxBase64Bytes*3/4 - 44) / 2

// Client uses the chat/completions API shared by MiMo ASR and TTS.
type Client struct {
	Key string
	URL string
	HTTP *http.Client
}

func NewClient(key, apiURL string) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" { key = os.Getenv("MIMO_API_KEY") }
	if key == "" { return nil, errors.New("MiMo API key is required") }
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" { apiURL = DefaultURL }
	endpoint, err := url.Parse(apiURL)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, errors.New("MiMo API 地址必须是包含有效主机名的 HTTP 或 HTTPS URL")
	}
	path := strings.TrimRight(endpoint.Path, "/")
	endpoint.Path = path
	endpoint.RawPath = strings.TrimRight(endpoint.RawPath, "/")
	if !strings.HasSuffix(path, "/chat/completions") {
		if strings.HasSuffix(path, "/v1") {
			endpoint = endpoint.JoinPath("chat/completions")
		} else {
			endpoint = endpoint.JoinPath("v1/chat/completions")
		}
	}
	return &Client{Key:key, URL:endpoint.String(), HTTP:&http.Client{Timeout:90*time.Second}}, nil
}

func (c *Client) request(ctx context.Context, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil { return nil, err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(payload))
	if err != nil { return nil, err }
	req.Header.Set("Authorization", "Bearer " + c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil { return nil, err }
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("MiMo HTTP %d: %s", resp.StatusCode, data)
	}
	return resp, nil
}

// WAV16 converts the application's mono float32 audio to PCM16 WAV (16 kHz).
func WAV16(pcm []float32, sampleRate int) ([]byte, error) {
	if sampleRate <= 0 { return nil, errors.New("invalid sample rate") }
	if len(pcm) == 0 { return nil, errors.New("empty audio") }
	if len(pcm) > MaxASRSamples {
		return nil, errors.New("MiMo ASR audio exceeds 10 MB base64 limit")
	}
	size := len(pcm)*2
	wav := make([]byte, 44+size)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(size+36))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(wav[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(size))
	for i, value := range pcm {
		if math.IsNaN(float64(value)) { value = 0 }
		if value > 1 { value = 1 }
		if value < -1 { value = -1 }
		var sample int16
		if value < 0 { sample = int16(value * 32768) } else { sample = int16(value * 32767) }
		binary.LittleEndian.PutUint16(wav[44+2*i:], uint16(sample))
	}
	return wav, nil
}

func (c *Client) Transcribe(ctx context.Context, pcm []float32, sampleRate int, language string) (string, error) {
	wav, err := WAV16(pcm, sampleRate)
	if err != nil { return "", err }
	if language == "" { language = "auto" }
	if language != "auto" && language != "zh" && language != "en" { return "", fmt.Errorf("invalid MiMo ASR language %q", language) }
	body := map[string]any{
		"model": "mimo-v2.5-asr",
		"messages": []any{map[string]any{"role":"user","content":[]any{map[string]any{"type":"input_audio","input_audio":map[string]string{"data":"data:audio/wav;base64,"+base64.StdEncoding.EncodeToString(wav)}}}}},
		"asr_options": map[string]string{"language":language},
	}
	resp, err := c.request(ctx,body)
	if err != nil { return "",err }
	defer resp.Body.Close()
	var out struct {
		Choices []struct { Message struct { Content string `json:"content"` } `json:"message"` } `json:"choices"`
		Error *struct { Message string `json:"message"` } `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body,2<<20)).Decode(&out); err != nil { return "",err }
	if out.Error != nil { return "",errors.New(out.Error.Message) }
	if len(out.Choices)==0 { return "",errors.New("MiMo ASR returned no choices") }
	return strings.TrimSpace(out.Choices[0].Message.Content),nil
}

// SpeechOptions chooses one of the three official MiMo TTS models.
type SpeechOptions struct {
	Model string
	Voice string
	Prompt string
	ReferenceAudioPath string
}

// Validate checks the options before the provider starts an asynchronous stream.
func (o SpeechOptions) Validate(text string) error {
	if strings.TrimSpace(text)=="" { return errors.New("MiMo TTS text is empty") }
	model:=o.Model
	if model=="" { model="mimo-v2.5-tts" }
	switch model {
	case "mimo-v2.5-tts":
		return nil
	case "mimo-v2.5-tts-voicedesign":
		if strings.TrimSpace(o.Prompt)=="" { return errors.New("MiMo VoiceDesign requires voice_prompt") }
	case "mimo-v2.5-tts-voiceclone":
		if o.ReferenceAudioPath=="" { return errors.New("MiMo VoiceClone requires reference_audio_path") }
		switch strings.ToLower(filepath.Ext(o.ReferenceAudioPath)) {
		case ".wav", ".mp3":
		default: return errors.New("MiMo reference audio must be WAV or MP3")
		}
		info,err:=os.Stat(o.ReferenceAudioPath)
		if err!=nil { return fmt.Errorf("stat MiMo reference audio: %w",err) }
		if !info.Mode().IsRegular() || info.Size()<=0 || info.Size()>MaxBase64Bytes*3/4 {
			return errors.New("MiMo reference audio must be a regular non-empty file below 7.5 MB")
		}
	default:
		return fmt.Errorf("unsupported MiMo TTS model %q",model)
	}
	return nil
}

func (o SpeechOptions) requestBody(text string) (map[string]any,error) {
	if err:=o.Validate(text);err!=nil { return nil,err }
	if o.Model=="" { o.Model="mimo-v2.5-tts" }
	audio:=map[string]any{"format":"pcm16"}
	messages:=[]any{}
	if o.Model=="mimo-v2.5-tts-voicedesign" {
		if strings.TrimSpace(o.Prompt)=="" { return nil,errors.New("MiMo VoiceDesign requires voice_prompt") }
		messages=append(messages,map[string]string{"role":"user","content":o.Prompt})
	} else {
		if o.Prompt!="" { messages=append(messages,map[string]string{"role":"user","content":o.Prompt}) }
		switch o.Model {
		case "mimo-v2.5-tts":
			if o.Voice=="" { o.Voice="mimo_default" }
			audio["voice"]=o.Voice
		case "mimo-v2.5-tts-voiceclone":
			if o.ReferenceAudioPath=="" { return nil,errors.New("MiMo VoiceClone requires reference_audio_path") }
			// Reject over-limit files before reading them into memory.
			info,err:=os.Stat(o.ReferenceAudioPath)
			if err!=nil { return nil,fmt.Errorf("stat MiMo reference audio: %w",err) }
			if !info.Mode().IsRegular() || info.Size()<=0 || info.Size()>MaxBase64Bytes*3/4 {
				return nil,errors.New("MiMo reference audio must be a regular non-empty file below 7.5 MB")
			}
			var mime string
			switch strings.ToLower(filepath.Ext(o.ReferenceAudioPath)) {
			case ".wav": mime="audio/wav"
			case ".mp3": mime="audio/mpeg"
			default: return nil,errors.New("MiMo reference audio must be WAV or MP3")
			}
			data,err:=os.ReadFile(o.ReferenceAudioPath)
			if err!=nil { return nil,fmt.Errorf("read MiMo reference audio: %w",err) }
			if (len(data)+2)/3*4>MaxBase64Bytes { return nil,errors.New("MiMo reference audio exceeds 10 MB base64 limit") }
			audio["voice"]="data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(data)
		default: return nil,fmt.Errorf("unsupported MiMo TTS model %q",o.Model)
		}
	}
	messages=append(messages,map[string]string{"role":"assistant","content":text})
	return map[string]any{"model":o.Model,"messages":messages,"audio":audio,"stream":true},nil
}

// StreamSpeech forwards decoded 24kHz mono PCM16LE chunks immediately. The
// existing decoder in the application converts the PCM stream to Opus.
func (c *Client) StreamSpeech(ctx context.Context,text string,options SpeechOptions,writer io.Writer) error {
	body,err:=options.requestBody(text)
	if err!=nil{return err}
	resp,err:=c.request(ctx,body)
	if err!=nil{return err}
	defer resp.Body.Close()
	reader:=bufio.NewReader(resp.Body)
	var event strings.Builder
	gotAudio:=false
	consume:=func(data string)(bool,error){
		if data=="[DONE]" {return true,nil}
		if data=="" {return false,nil}
		var frame struct {
			Choices []struct { Delta struct { Audio *struct { Data string `json:"data"` } `json:"audio"` } `json:"delta"` } `json:"choices"`
			Error *struct { Message string `json:"message"` } `json:"error"`
		}
		if err:=json.Unmarshal([]byte(data),&frame);err!=nil{return false,fmt.Errorf("MiMo SSE: %w",err)}
		if frame.Error!=nil{return false,errors.New(frame.Error.Message)}
		for _,choice:=range frame.Choices {
			if choice.Delta.Audio==nil||choice.Delta.Audio.Data=="" {continue}
			pcm,err:=base64.StdEncoding.DecodeString(choice.Delta.Audio.Data)
			if err!=nil{return false,fmt.Errorf("MiMo PCM16: %w",err)}
			if _,err=writer.Write(pcm);err!=nil{return false,err}
			gotAudio=true
		}
		return false,nil
	}
	for {
		line,readErr:=reader.ReadString('\n')
		if len(line)>0 {
			line=strings.TrimRight(line,"\r\n")
			if line=="" {
				stop,err:=consume(strings.TrimSpace(event.String()))
				event.Reset()
				if err!=nil{return err}
				if stop{break}
			} else if strings.HasPrefix(line,"data:"){
				if event.Len()>0{event.WriteByte('\n')}
				event.WriteString(strings.TrimSpace(strings.TrimPrefix(line,"data:")))
			}
		}
		if readErr==io.EOF{break}
		if readErr!=nil{return readErr}
	}
	if event.Len()>0 {
		if _,err:=consume(strings.TrimSpace(event.String()));err!=nil{return err}
	}
	if !gotAudio{return errors.New("MiMo TTS returned no audio")}
	return nil
}
