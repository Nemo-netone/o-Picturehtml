//  通话详单(CDR)：记录每次通话的起止时间、时长、ASR/TMT/TTS消耗
package cdr

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

var ErrNotFound = errors.New("cdr not found")

type Record struct {
	TenantID   string
	CallID     string
	Caller     string
	Callee     string
	State      model.CallState
	StartedAt  time.Time
	AnsweredAt time.Time
	EndedAt    time.Time
	Duration   time.Duration
	Summary    string
}

type Service struct {
	mu      sync.Mutex
	records map[string]Record
}

// NewService 创建内存 CDR 服务。
func NewService() *Service {
	return &Service{records: map[string]Record{}}
}

// Create 创建一条 CDR 记录，默认状态为 ringing。
func (s *Service) Create(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now()
	}
	if record.State == "" {
		record.State = model.CallStateRinging
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key(record.TenantID, record.CallID)] = record
	return nil
}

// MarkAnswered 标记通话已应答，记录应答时间、状态改为 connected。
func (s *Service) MarkAnswered(ctx context.Context, tenantID, callID string, answeredAt time.Time) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key(tenantID, callID)]
	if !ok {
		return Record{}, ErrNotFound
	}
	record.AnsweredAt = answeredAt
	record.State = model.CallStateConnected
	s.records[key(tenantID, callID)] = record
	return record, nil
}

// End 标记通话结束，计算通话时长（从应答时间起算，未应答则从开始起算）。
func (s *Service) End(ctx context.Context, tenantID, callID string, endedAt time.Time) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key(tenantID, callID)]
	if !ok {
		return Record{}, ErrNotFound
	}
	record.EndedAt = endedAt
	record.State = model.CallStateEnded
	start := record.AnsweredAt
	if start.IsZero() {
		start = record.StartedAt
	}
	if !start.IsZero() && endedAt.After(start) {
		record.Duration = endedAt.Sub(start)
	}
	if record.Summary == "" {
		record.Summary = "summary pending"
	}
	s.records[key(tenantID, callID)] = record
	return record, nil
}

// Get 按租户+通话 ID 查询 CDR。
func (s *Service) Get(ctx context.Context, tenantID, callID string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key(tenantID, callID)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}

// key 生成 CDR 存储键：tenantID:callID。
func key(tenantID, callID string) string {
	return tenantID + ":" + callID
}

