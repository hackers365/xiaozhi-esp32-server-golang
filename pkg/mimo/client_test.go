package mimo

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWAV16(t *testing.T) {
	data,err:=WAV16([]float32{-1,0,1},16000)
	if err!=nil{t.Fatal(err)}
	if len(data)!=50 || string(data[:4])!="RIFF" || string(data[8:12])!="WAVE" || binary.LittleEndian.Uint32(data[24:])!=16000 {
		t.Fatalf("unexpected WAV header: %v",data)
	}
}

func TestASRRequest(t *testing.T) {
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.Header.Get("Authorization")!="Bearer test"{t.Error("missing authentication")}
		var v struct {
			Model string `json:"model"`
			ASROptions struct { Language string `json:"language"` } `json:"asr_options"`
			Messages []struct { Content []struct { InputAudio struct {Data string `json:"data"`} `json:"input_audio"`} `json:"content"`} `json:"messages"`
		}
		if err:=json.NewDecoder(r.Body).Decode(&v);err!=nil{t.Error(err);return}
		if v.Model!="mimo-v2.5-asr" || v.ASROptions.Language!="zh" || len(v.Messages)==0 || len(v.Messages[0].Content)==0 || !strings.HasPrefix(v.Messages[0].Content[0].InputAudio.Data,"data:audio/wav;base64,"){
			t.Errorf("invalid MiMo ASR request: %+v",v)
		}
		_,_=io.WriteString(w,`{"choices":[{"message":{"content":"你好。"}}]}`)
	}))
	defer server.Close()
	client,err:=NewClient("test",server.URL)
	if err!=nil{t.Fatal(err)}
	got,err:=client.Transcribe(context.Background(),[]float32{0.1,-0.2},16000,"zh")
	if err!=nil||got!="你好。"{t.Fatalf("text=%q, err=%v",got,err)}
}

func TestStreamSpeech(t *testing.T) {
	pcm:=[]byte{0,1,2,3}
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		var v struct {
			Model string `json:"model"`
			Stream bool `json:"stream"`
			Audio struct { Format string `json:"format"`;Voice string `json:"voice"`} `json:"audio"`
			Messages []struct {Role string `json:"role"`;Content string `json:"content"`} `json:"messages"`
		}
		if err:=json.NewDecoder(r.Body).Decode(&v);err!=nil{t.Error(err);return}
		if v.Model!="mimo-v2.5-tts"||!v.Stream||v.Audio.Format!="pcm16"||v.Audio.Voice!="mimo_default"||len(v.Messages)!=1||v.Messages[0].Role!="assistant" {
			t.Errorf("invalid MiMo TTS request: %+v",v)
		}
		w.Header().Set("Content-Type","text/event-stream")
		_,_=io.WriteString(w,`data: {"choices":[{"delta":{"audio":{"data":"`+base64.StdEncoding.EncodeToString(pcm)+`"}}}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	client,err:=NewClient("test",server.URL)
	if err!=nil{t.Fatal(err)}
	var out strings.Builder
	err=client.StreamSpeech(context.Background(),"你好",SpeechOptions{},&out)
	if err!=nil||out.String()!=string(pcm){t.Fatalf("PCM=%v err=%v",[]byte(out.String()),err)}
}

func TestVoiceModels(t *testing.T){
	design,err:=(SpeechOptions{Model:"mimo-v2.5-tts-voicedesign",Prompt:"温柔的女声"}).requestBody("你好")
	if err!=nil{t.Fatal(err)}
	if design["model"]!="mimo-v2.5-tts-voicedesign"{t.Fatal("wrong model")}
	if _,err=(SpeechOptions{Model:"mimo-v2.5-tts-voicedesign"}).requestBody("hi");err==nil{t.Fatal("missing prompt must fail")}
	if _,err=(SpeechOptions{Model:"mimo-v2.5-tts-voiceclone"}).requestBody("hi");err==nil{t.Fatal("missing audio must fail")}
	path:=filepath.Join(t.TempDir(),"voice.wav")
	if err=os.WriteFile(path,[]byte("RIFFdemo"),0600);err!=nil{t.Fatal(err)}
	clone,err:=(SpeechOptions{Model:"mimo-v2.5-tts-voiceclone",ReferenceAudioPath:path}).requestBody("hi")
	if err!=nil{t.Fatal(err)}
	audio:=clone["audio"].(map[string]any)
	if !strings.HasPrefix(audio["voice"].(string),"data:audio/wav;base64,"){t.Fatal("reference audio not embedded")}
}

func TestASRBase64Limit(t *testing.T) {
	_, err := WAV16(make([]float32, MaxASRSamples+1), 16000)
	if err == nil { t.Fatal("ASR audio above the 10MB encoded limit must be rejected") }
}

func TestInvalidSpeechOptions(t *testing.T) {
	if err := (SpeechOptions{Model:"unknown"}).Validate("hello"); err == nil {
		t.Fatal("unknown model must fail before streaming")
	}
	if err := (SpeechOptions{Model:"mimo-v2.5-tts-voicedesign"}).Validate("hello"); err == nil {
		t.Fatal("VoiceDesign requires a prompt")
	}
	if err := (SpeechOptions{Model:"mimo-v2.5-tts-voiceclone",ReferenceAudioPath:"/not/a/real/file.wav"}).Validate("hello"); err == nil {
		t.Fatal("VoiceClone needs a real audio file")
	}
}
