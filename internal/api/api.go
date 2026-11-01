package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Servitor108/go-webhook-platform/internal/store"
)

type API struct { store *store.Store; log *slog.Logger }
func New(s *store.Store,l *slog.Logger)*API{return &API{store:s,log:l}}
func (a *API) Register(m *http.ServeMux) {
	m.HandleFunc("GET /healthz",func(w http.ResponseWriter,r *http.Request){write(w,200,map[string]string{"status":"ok"})})
	m.HandleFunc("POST /v1/endpoints",a.createEndpoint)
	m.HandleFunc("GET /v1/endpoints",a.listEndpoints)
	m.HandleFunc("POST /v1/events",a.publish)
}
func (a *API) createEndpoint(w http.ResponseWriter,r *http.Request){
	var in struct{URL string `json:"url"`}
	if err:=decode(r,&in);err!=nil{writeError(w,400,"invalid JSON");return}
	u,err:=url.ParseRequestURI(in.URL)
	if err!=nil || (u.Scheme!="https" && u.Scheme!="http") || u.Host=="" {writeError(w,400,"url must be an absolute http(s) URL");return}
	secret,err:=newSecret();if err!=nil{writeError(w,500,"could not create signing secret");return}
	e,err:=a.store.CreateEndpoint(r.Context(),in.URL,secret);if err!=nil{a.log.Error("create endpoint","error",err);writeError(w,500,"database error");return}
	write(w,201,e)
}
func (a *API) listEndpoints(w http.ResponseWriter,r *http.Request){
	items,err:=a.store.ListEndpoints(r.Context());if err!=nil{writeError(w,500,"database error");return};write(w,200,map[string]any{"endpoints":items})
}
func (a *API) publish(w http.ResponseWriter,r *http.Request){
	var in struct{Type string `json:"type"`;Data json.RawMessage `json:"data"`}
	if err:=decode(r,&in);err!=nil{writeError(w,400,"invalid JSON");return}
	if strings.TrimSpace(in.Type)=="" || len(in.Data)==0 || !json.Valid(in.Data){writeError(w,400,"type and valid JSON data are required");return}
	key:=strings.TrimSpace(r.Header.Get("Idempotency-Key"));if key==""{writeError(w,400,"Idempotency-Key header is required");return}
	e,err:=a.store.Publish(r.Context(),in.Type,in.Data,key);if err!=nil{a.log.Error("publish event","error",err);writeError(w,500,"could not publish event");return}
	write(w,202,e)
}
func decode(r *http.Request,v any)error{r.Body=http.MaxBytesReader(nil,r.Body,1<<20);defer r.Body.Close();d:=json.NewDecoder(r.Body);d.DisallowUnknownFields();if err:=d.Decode(v);err!=nil{return err};if err:=d.Decode(&struct{}{});err!=io.EOF{return err};return nil}
func write(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(v)}
func writeError(w http.ResponseWriter,status int,msg string){write(w,status,map[string]string{"error":msg})}
