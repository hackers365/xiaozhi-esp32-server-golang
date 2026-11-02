package mimo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gopxl/beep"

	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
	mimoclient "xiaozhi-esp32-server-golang/pkg/mimo"
)

// Provider implements the existing TTS interfaces without modifying the audio pipeline.
type Provider struct {
	client *mimoclient.Client
	mu sync.RWMutex
	options mimoclient.SpeechOptions
}

func NewProvider(config map[string]interface{}) *Provider {
	key,_:=config["api_key"].(string)
	url,_:=config["api_url"].(string)
	client,err:=mimoclient.NewClient(key,url)
	if err!=nil{log.Errorf("MiMo TTS config: %v",err);return nil}
	model,_:=config["model"].(string)
	voice,_:=config["voice"].(string)
	prompt,_:=config["voice_prompt"].(string)
	reference,_:=config["reference_audio_path"].(string)
	return &Provider{client:client,options:mimoclient.SpeechOptions{Model:model,Voice:voice,Prompt:prompt,ReferenceAudioPath:reference}}
}

func(p *Provider) TextToSpeech(ctx context.Context,text string,rate int,channels int,duration int)([][]byte,error){
	output,err:=p.TextToSpeechStream(ctx,text,rate,channels,duration)
	if err!=nil{return nil,err}
	var frames [][]byte
	for frame:=range output{frames=append(frames,frame)}
	if len(frames)==0{return nil,errors.New("MiMo TTS returned empty audio")}
	return frames,nil
}

func(p *Provider) TextToSpeechStream(ctx context.Context,text string,rate int,channels int,duration int)(chan []byte,error){
	if p==nil||p.client==nil{return nil,errors.New("MiMo TTS provider is not initialized")}
	if duration<=0{duration=60}
	if rate<=0{rate=24000}
	p.mu.RLock()
	options:=p.options
	p.mu.RUnlock()
	if err:=options.Validate(text);err!=nil{return nil,err}
	reader,writer:=io.Pipe()
	output:=make(chan []byte,100)
	decoder,err:=util.CreateAudioDecoderWithSampleRate(ctx,reader,output,duration,"pcm",rate)
	if err!=nil{reader.Close();writer.Close();return nil,err}
	decoder.WithFormat(beep.Format{SampleRate:beep.SampleRate(24000),NumChannels:1,Precision:2})
	go func(){
		defer writer.Close()
		if err:=p.client.StreamSpeech(ctx,text,options,writer);err!=nil{
			log.Errorf("MiMo TTS synthesis failed: %v",err)
			_ = writer.CloseWithError(err)
		}
	}()
	go func(){
		if err:=decoder.Run(time.Now().UnixMilli());err!=nil{
			log.Errorf("MiMo TTS audio conversion failed: %v",err)
		}
	}()
	return output,nil
}

func(p *Provider) SetVoice(config map[string]interface{})error{
	if p==nil{return fmt.Errorf("MiMo TTS provider is nil")}
	p.mu.Lock()
	defer p.mu.Unlock()
	if voice,ok:=config["voice"].(string);ok&&voice!=""{p.options.Voice=voice}
	return nil
}
func(p *Provider) Close()error{return nil}
func(p *Provider) IsValid()bool{return p!=nil&&p.client!=nil}
