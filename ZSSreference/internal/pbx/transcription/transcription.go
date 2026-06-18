//  转录：ASR结果持久化与导出
package transcription

import (
	"context"
	"strings"
	"sync"

	"github.com/SATA260/SimulSpeak1/internal/model"
)

type Service struct {
	mu      sync.Mutex
	entries map[string][]model.ASRResult
}

// NewService 创建录音服务。
func NewService() *Service {
	return &Service{entries: map[string][]model.ASRResult{}}
}

// Write 写入转写结果。
func (s *Service) Write(ctx context.Context, tenantID string, result model.ASRResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantID + ":" + result.CallID
	s.entries[key] = append(s.entries[key], result)
	return nil
}

// SearchByCall 查询某通话的所有转写结果。
func (s *Service) SearchByCall(ctx context.Context, tenantID, callID string) ([]model.ASRResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantID + ":" + callID
	return append([]model.ASRResult(nil), s.entries[key]...), nil
}

// SearchText 全文搜索转写内容。
func (s *Service) SearchText(ctx context.Context, tenantID, query string) ([]model.ASRResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.ASRResult
	for key, entries := range s.entries {
		if !strings.HasPrefix(key, tenantID+":") {
			continue
		}
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Text), strings.ToLower(query)) {
				out = append(out, entry)
			}
		}
	}
	return out, nil
}

