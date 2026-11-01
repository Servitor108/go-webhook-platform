package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct { db *pgxpool.Pool }
func New(db *pgxpool.Pool) *Store { return &Store{db:db} }

type Endpoint struct { ID string `json:"id"`; URL string `json:"url"`; Secret string `json:"secret,omitempty"` }
type Event struct { ID string `json:"id"`; Type string `json:"type"`; Data json.RawMessage `json:"data"`; CreatedAt time.Time `json:"created_at"` }
type Delivery struct { ID string; EventID string; EndpointID string; URL string; Secret string; EventType string; Payload []byte; Attempts int }

func (s *Store) CreateEndpoint(ctx context.Context, url, secret string) (Endpoint,error) {
	var e Endpoint
	err:=s.db.QueryRow(ctx,`INSERT INTO endpoints(url,secret) VALUES($1,$2) RETURNING id::text,url,secret`,url,secret).Scan(&e.ID,&e.URL,&e.Secret)
	return e,err
}
func (s *Store) ListEndpoints(ctx context.Context) ([]Endpoint,error) {
	rows,err:=s.db.Query(ctx,`SELECT id::text,url FROM endpoints WHERE enabled=true ORDER BY created_at DESC`)
	if err!=nil{return nil,err}; defer rows.Close()
	out:=[]Endpoint{}
	for rows.Next(){var e Endpoint;if err:=rows.Scan(&e.ID,&e.URL);err!=nil{return nil,err};out=append(out,e)}
	return out,rows.Err()
}
func (s *Store) Publish(ctx context.Context, eventType string, data json.RawMessage, key string) (Event,error) {
	tx,err:=s.db.Begin(ctx);if err!=nil{return Event{},err};defer tx.Rollback(ctx)
	var e Event
	err=tx.QueryRow(ctx,`INSERT INTO events(event_type,payload,idempotency_key) VALUES($1,$2,$3)
		ON CONFLICT(idempotency_key) DO NOTHING RETURNING id::text,event_type,payload,created_at`,eventType,data,key).Scan(&e.ID,&e.Type,&e.Data,&e.CreatedAt)
	if errors.Is(err,pgx.ErrNoRows) {
		err=tx.QueryRow(ctx,`SELECT id::text,event_type,payload,created_at FROM events WHERE idempotency_key=$1`,key).Scan(&e.ID,&e.Type,&e.Data,&e.CreatedAt)
		if err!=nil{return Event{},err}; if err:=tx.Commit(ctx);err!=nil{return Event{},err};return e,nil
	}
	if err!=nil{return Event{},err}
	_,err=tx.Exec(ctx,`INSERT INTO deliveries(event_id,endpoint_id)
		SELECT $1,id FROM endpoints WHERE enabled=true`,e.ID)
	if err!=nil{return Event{},err}
	if err:=tx.Commit(ctx);err!=nil{return Event{},err}
	return e,nil
}
func (s *Store) Claim(ctx context.Context) (Delivery,error) {
	tx,err:=s.db.Begin(ctx);if err!=nil{return Delivery{},err};defer tx.Rollback(ctx)
	var d Delivery
	err=tx.QueryRow(ctx,`SELECT d.id::text,d.event_id::text,d.endpoint_id::text,e.url,e.secret,v.event_type,v.payload,d.attempts
		FROM deliveries d JOIN endpoints e ON e.id=d.endpoint_id JOIN events v ON v.id=d.event_id
		WHERE d.status IN ('pending','retry') AND d.next_attempt_at<=now()
		ORDER BY d.next_attempt_at FOR UPDATE OF d SKIP LOCKED LIMIT 1`).Scan(&d.ID,&d.EventID,&d.EndpointID,&d.URL,&d.Secret,&d.EventType,&d.Payload,&d.Attempts)
	if err!=nil{return Delivery{},err}
	_,err=tx.Exec(ctx,`UPDATE deliveries SET status='processing',attempts=attempts+1,updated_at=now() WHERE id=$1`,d.ID)
	if err!=nil{return Delivery{},err}
	if err:=tx.Commit(ctx);err!=nil{return Delivery{},err};d.Attempts++;return d,nil
}
func (s *Store) Finish(ctx context.Context,id,status string,next time.Time,lastError string) error {
	_,err:=s.db.Exec(ctx,`UPDATE deliveries SET status=$2,next_attempt_at=$3,last_error=NULLIF($4,''),updated_at=now() WHERE id=$1`,id,status,next,lastError);return err
}
func (s *Store) RetryExpired(ctx context.Context) error {
	_,err:=s.db.Exec(ctx,`UPDATE deliveries SET status='retry',updated_at=now() WHERE status='processing' AND updated_at < now()-interval '1 minute'`);return err
}
