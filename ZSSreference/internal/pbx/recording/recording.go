//  录音：音频流录制与存储
package recording

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SATA260/SimulSpeak1/internal/pbx/storage"
)

var (
	ErrNotFound     = errors.New("recording not found")
	ErrAccessDenied = errors.New("recording tenant access denied")
)

type Metadata struct {
	ID         string
	TenantID   string
	CallID     string
	ObjectKey  string
	Checksum   string
	Size       int64
	StartedAt  time.Time
	StoppedAt  time.Time
	Active     bool
	LegalHold  bool
	UploadedAt time.Time
}

type RetentionPolicy struct {
	TenantID      string
	RetentionDays int
}

type AuditRecord struct {
	TenantID    string
	RecordingID string
	Action      string
	Outcome     string
}

type Service struct {
	mu       sync.Mutex
	storage  storage.ObjectStorage
	metadata map[string]Metadata
	audit    []AuditRecord
}

// NewService 创建录音服务。
func NewService(objectStorage storage.ObjectStorage) *Service {
	if objectStorage == nil {
		objectStorage = storage.NewMemoryStorage()
	}
	return &Service{storage: objectStorage, metadata: map[string]Metadata{}, audit: []AuditRecord{}}
}

// Start 开始录音，记录元数据。
func (s *Service) Start(ctx context.Context, tenantID, callID, recordingID string, startedAt time.Time) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	metadata := Metadata{
		ID:        recordingID,
		TenantID:  tenantID,
		CallID:    callID,
		ObjectKey: objectKey(tenantID, callID, recordingID),
		StartedAt: startedAt,
		Active:    true,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata[recordingID] = metadata
	return metadata, nil
}

// CompleteUpload 上传录音数据到对象存储。
// 逻辑：先校验租户归属，再写入对象存储；成功后补齐停止时间、上传时间、校验和和大小，并更新元数据。
func (s *Service) CompleteUpload(ctx context.Context, tenantID, recordingID string, audio []byte, expectedChecksum string, stoppedAt time.Time) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	metadata, err := s.getOwned(tenantID, recordingID)
	if err != nil {
		return Metadata{}, err
	}
	object, err := s.storage.Put(ctx, metadata.ObjectKey, audio, storage.PutOptions{ContentType: "audio/wav", Checksum: expectedChecksum})
	if err != nil {
		return Metadata{}, err
	}
	if stoppedAt.IsZero() {
		stoppedAt = time.Now()
	}
	metadata.Active = false
	metadata.StoppedAt = stoppedAt
	metadata.UploadedAt = time.Now()
	metadata.Checksum = object.Checksum
	metadata.Size = object.Size
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata[recordingID] = metadata
	return metadata, nil
}

// Get 读取。
func (s *Service) Get(ctx context.Context, tenantID, recordingID string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	metadata, err := s.getOwned(tenantID, recordingID)
	if err != nil {
		outcome := "denied"
		if errors.Is(err, ErrNotFound) {
			outcome = "not_found"
		}
		s.recordAudit(AuditRecord{TenantID: tenantID, RecordingID: recordingID, Action: "get", Outcome: outcome})
		return Metadata{}, err
	}
	s.recordAudit(AuditRecord{TenantID: tenantID, RecordingID: recordingID, Action: "get", Outcome: "success"})
	return metadata, nil
}

// SetLegalHold 设置/取消录音的法律保留标记。
func (s *Service) SetLegalHold(tenantID, recordingID string, hold bool) error {
	metadata, err := s.getOwned(tenantID, recordingID)
	if err != nil {
		return err
	}
	metadata.LegalHold = hold
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata[recordingID] = metadata
	return nil
}

// ApplyRetention 按保留天数删除过期录音。
// 逻辑：先筛选同租户、无法律保留且超过保留期的候选录音，再逐个删除对象和对应元数据。
func (s *Service) ApplyRetention(ctx context.Context, now time.Time, policy RetentionPolicy) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if policy.RetentionDays <= 0 {
		return nil, nil
	}
	cutoff := now.Add(-time.Duration(policy.RetentionDays) * 24 * time.Hour)
	var candidates []Metadata
	s.mu.Lock()
	for _, metadata := range s.metadata {
		if metadata.TenantID != policy.TenantID || metadata.LegalHold || metadata.StoppedAt.IsZero() || metadata.StoppedAt.After(cutoff) {
			continue
		}
		candidates = append(candidates, metadata)
	}
	s.mu.Unlock()

	deleted := make([]string, 0, len(candidates))
	for _, metadata := range candidates {
		if err := s.storage.Delete(ctx, metadata.ObjectKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
			return deleted, err
		}
		s.mu.Lock()
		delete(s.metadata, metadata.ID)
		s.mu.Unlock()
		deleted = append(deleted, metadata.ID)
	}
	return deleted, nil
}

// AuditLog 返回审计日志副本。
func (s *Service) AuditLog() []AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AuditRecord(nil), s.audit...)
}

// getOwned 读取并校验录音的租户归属。
func (s *Service) getOwned(tenantID, recordingID string) (Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	metadata, ok := s.metadata[recordingID]
	if !ok {
		return Metadata{}, ErrNotFound
	}
	if metadata.TenantID != tenantID {
		return Metadata{}, ErrAccessDenied
	}
	return metadata, nil
}

// recordAudit 追加审计记录。
func (s *Service) recordAudit(record AuditRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, record)
}

// objectKey 生成对象存储路径：recordings/{tenant}/{call}/{recordingID}.wav。
func objectKey(tenantID, callID, recordingID string) string {
	return "recordings/" + tenantID + "/" + callID + "/" + recordingID + ".wav"
}

