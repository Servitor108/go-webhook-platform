package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Servitor108/go-webhook-platform/internal/store"
)

type Worker struct { store *store.Store; log *slog.Logger; concurrency int; timeout time.Duration; maxAttempts int; client *http.Client }
func NewWorker(s *store.Store,l *slog.Logger,n int,t time.Duration,max int)*Worker{return &Worker{store:s,log:l,concurrency:n,timeout:t,maxAttempts:max,client:&http.Client{Timeout:t}}}
func (w *Worker) Run(ctx context.Context){
	var wg sync.WaitGroup
	for i:=0;i<w.concurrency;i++{wg.Add(1);go func(){defer wg.Done();w.loop(ctx)}()}
	<-ctx.Done();wg.Wait()
}
func (w *Worker) loop(ctx context.Context){
	ticker:=time.NewTicker(300*time.Millisecond);defer ticker.Stop()
	for{select{case <-ctx.Done():return;case <-ticker.C:
		if err:=w.store.RetryExpired(ctx);err!=nil{w.log.Error("recover expired deliveries","error",err)}
		d,err:=w.store.Claim(ctx);if err!=nil{continue};w.deliver(ctx,d)
	}}
}
func (w *Worker) deliver(parent context.Context,d store.Delivery){
	ctx,cancel:=context.WithTimeout(parent,w.timeout);defer cancel()
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,d.URL,bytes.NewReader(d.Payload))
	if err==nil{
		req.Header.Set("Content-Type","application/json");req.Header.Set("X-Webhook-Event",d.EventType);req.Header.Set("X-Webhook-Delivery",d.ID)
		req.Header.Set("X-Webhook-Signature",sign(d.Secret,d.Payload))
		var resp *http.Response
		resp,err=w.client.Do(req)
		if err==nil{io.Copy(io.Discard,io.LimitReader(resp.Body,4096));resp.Body.Close();if resp.StatusCode<200||resp.StatusCode>=300{err=fmt.Errorf("receiver returned HTTP %d",resp.StatusCode)}}
	}
	if err==nil{if e:=w.store.Finish(parent,d.ID,"succeeded",time.Now(),"");e!=nil{w.log.Error("mark delivery succeeded","error",e)};return}
	status:="retry";next:=time.Now().Add(backoff(d.Attempts));if d.Attempts>=w.maxAttempts{status="failed";next=time.Now()}
	if e:=w.store.Finish(parent,d.ID,status,next,err.Error());e!=nil{w.log.Error("update delivery failure","error",e)}
}
func sign(secret string,payload []byte)string{m:=hmac.New(sha256.New,[]byte(secret));m.Write(payload);return "sha256="+hex.EncodeToString(m.Sum(nil))}
func backoff(attempt int)time.Duration{seconds:=math.Min(3600,math.Pow(2,float64(attempt)));jitter:=0.8+rand.Float64()*0.4;return time.Duration(seconds*jitter*float64(time.Second))}
func ParseSignature(s string)bool{return len(s)>7&&s[:7]=="sha256="&&len(s[7:])==64&&func()bool{_,e:=hex.DecodeString(s[7:]);return e==nil}()}
func AttemptHeader(n int)string{return strconv.Itoa(n)}
