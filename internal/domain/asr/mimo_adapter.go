package asr

import (
	"context"
	"errors"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/asr/types"
	"xiaozhi-esp32-server-golang/pkg/mimo"
)

// MiMoASRAdapter buffers one VAD-delimited utterance before uploading WAV.
// MiMo's SSE response does not support streaming audio *input*.
type MiMoASRAdapter struct {
	client *mimo.Client
	language string
	sampleRate int
	timeout time.Duration
}

func NewMiMoASRAdapter(config map[string]interface{}) (AsrProvider,error) {
	key,_:=config["api_key"].(string)
	url,_:=config["api_url"].(string)
	client,err:=mimo.NewClient(key,url)
	if err!=nil{return nil,err}
	lang,_:=config["language"].(string)
	if lang==""{lang="auto"}
	if lang!="auto"&&lang!="zh"&&lang!="en"{return nil,errors.New("MiMo language must be auto, zh or en")}
	rate:=16000
	if v,ok:=config["sample_rate"].(float64);ok&&v>0{rate=int(v)}
	if v,ok:=config["sample_rate"].(int);ok&&v>0{rate=v}
	timeout:=60*time.Second
	if v,ok:=config["timeout"].(float64);ok&&v>0{timeout=time.Duration(v)*time.Second}
	if v,ok:=config["timeout"].(int);ok&&v>0{timeout=time.Duration(v)*time.Second}
	return &MiMoASRAdapter{client:client,language:lang,sampleRate:rate,timeout:timeout},nil
}

func (a *MiMoASRAdapter) Process(pcm []float32)(string,error){
	ctx,cancel:=context.WithTimeout(context.Background(),a.timeout)
	defer cancel()
	return a.client.Transcribe(ctx,pcm,a.sampleRate,a.language)
}

func (a *MiMoASRAdapter) StreamingRecognize(ctx context.Context,audio <-chan []float32)(chan types.StreamingResult,error){
	result:=make(chan types.StreamingResult,1)
	go func(){
		defer close(result)
		samples:=make([]float32,0,16000)
		for {
			select {
			case <-ctx.Done():return
			case frame,ok:=<-audio:
				if !ok {
					if len(samples)==0{return}
					reqCtx,cancel:=context.WithTimeout(ctx,a.timeout)
					text,err:=a.client.Transcribe(reqCtx,samples,a.sampleRate,a.language)
					cancel()
					select {
					case result<-types.StreamingResult{Text:text,IsFinal:true,Error:err,AsrType:"mimo",Mode:"offline"}:
					case <-ctx.Done():
					}
					return
				}
				if len(samples)+len(frame)>mimo.MaxASRSamples {
					select {
					case result<-types.StreamingResult{IsFinal:true,Error:errors.New("MiMo ASR audio exceeds limit"),AsrType:"mimo",Mode:"offline"}:
					case <-ctx.Done():
					}
					return
				}
				samples=append(samples,frame...)
			}
		}
	}()
	return result,nil
}
func (a *MiMoASRAdapter) Close()error{return nil}
func (a *MiMoASRAdapter) IsValid()bool{return a!=nil&&a.client!=nil}
