//  通话会话管理：会话CRUD+状态机转换
package session

import (
	"context"
	"errors"
	"sort"

	pbxerrors "github.com/SATA260/SimulSpeak1/internal/errors"
	"github.com/SATA260/SimulSpeak1/internal/etcdutil"
	"github.com/SATA260/SimulSpeak1/internal/model"
)

const callRoot = "/pbx/calls"

var ErrDuplicateCallID = errors.New("duplicate call id")

type Manager struct {
	client etcdutil.Client
}

type CreateRequest struct {
	TenantID     string
	Caller       string
	Callee       string
	OwnerNode    string
	GatewayNode  string
	MediaNode    string
	TurnNode     string
	Participants []model.Participant
	AIPipeline   *model.AIPipeline
}

type Update struct {
	State *model.CallState
	Media *model.MediaState
}

type Filter struct {
	TenantID string
	State    model.CallState
	NodeID   string
}

// New 创建通话会话管理器。
func New(client etcdutil.Client) *Manager {
	return &Manager{client: client}
}

// CreateSession 在 etcd 中原子创建一条通话记录（含元数据和 owner），callID 重复时返回 ErrDuplicateCallID。
func (m *Manager) CreateSession(ctx context.Context, callID string, req CreateRequest) (*model.CallSession, error) {
	owner := model.CallOwner{
		CallID:      callID,
		OwnerNode:   req.OwnerNode,
		GatewayNode: req.GatewayNode,
		Epoch:       1,
	}
	call := model.CallSession{
		ID:           callID,
		TenantID:     req.TenantID,
		Caller:       req.Caller,
		Callee:       req.Callee,
		State:        model.CallStateRinging,
		Media:        model.MediaStateRinging,
		Owner:        owner,
		GatewayNode:  req.GatewayNode,
		MediaNode:    req.MediaNode,
		TurnNode:     req.TurnNode,
		Participants: req.Participants,
		AIPipeline:   req.AIPipeline,
	}

	ownerData, err := etcdutil.Marshal(owner)
	if err != nil {
		return nil, err
	}
	if err := m.client.CreateIfNotExists(ctx, ownerKey(callID), ownerData); errors.Is(err, etcdutil.ErrKeyExists) {
		return nil, ErrDuplicateCallID
	} else if err != nil {
		return nil, err
	}

	data, err := etcdutil.Marshal(call)
	if err != nil {
		return nil, err
	}
	if err := m.client.CreateIfNotExists(ctx, metaKey(callID), data); err != nil {
		return nil, err
	}
	return &call, nil
}

// GetSession 从 etcd 读取一条通话记录。
func (m *Manager) GetSession(ctx context.Context, callID string) (*model.CallSession, error) {
	var call model.CallSession
	if err := m.get(ctx, metaKey(callID), &call); err != nil {
		return nil, err
	}
	return &call, nil
}

// UpdateSession 乐观更新通话状态和/或媒体状态：校验 epoch（防脑裂）和状态转移合法性后写入 etcd。
func (m *Manager) UpdateSession(ctx context.Context, callID string, epoch int64, update Update) error {
	call, item, err := m.getSessionItem(ctx, callID)
	if err != nil {
		return err
	}
	if call.Owner.Epoch != epoch {
		return pbxerrors.ErrEpochMismatch
	}
	if update.State != nil {
		if !model.CanTransitionCallState(call.State, *update.State) {
			return model.ValidateCallTransition(call.State, *update.State)
		}
		call.State = *update.State
	}
	if update.Media != nil {
		call.Media = *update.Media
	}
	data, err := etcdutil.Marshal(call)
	if err != nil {
		return err
	}
	return m.client.UpdateIfVersion(ctx, metaKey(callID), item.Version, data)
}

// TransferOwner 将通话所有权转移给另一节点，epoch 递增（防止旧 owner 继续操作）。
func (m *Manager) TransferOwner(ctx context.Context, callID, newOwnerNode string) (model.CallOwner, error) {
	call, item, err := m.getSessionItem(ctx, callID)
	if err != nil {
		return model.CallOwner{}, err
	}
	call.Owner.OwnerNode = newOwnerNode
	call.Owner.Epoch++
	ownerData, err := etcdutil.Marshal(call.Owner)
	if err != nil {
		return model.CallOwner{}, err
	}
	if _, err := m.client.Put(ctx, ownerKey(callID), ownerData); err != nil {
		return model.CallOwner{}, err
	}
	data, err := etcdutil.Marshal(call)
	if err != nil {
		return model.CallOwner{}, err
	}
	if err := m.client.UpdateIfVersion(ctx, metaKey(callID), item.Version, data); err != nil {
		return model.CallOwner{}, err
	}
	return call.Owner, nil
}

// EndSession 将通话状态和媒体状态都设为 ended（通过 UpdateSession 校验 epoch）。
func (m *Manager) EndSession(ctx context.Context, callID string, epoch int64) error {
	state := model.CallStateEnded
	media := model.MediaStateEnded
	return m.UpdateSession(ctx, callID, epoch, Update{State: &state, Media: &media})
}

// ListSessions 列出所有通话，按 tenant/状态/节点过滤，结果按 callID 排序。
func (m *Manager) ListSessions(ctx context.Context, filter Filter) ([]model.CallSession, error) {
	items, err := m.client.GetPrefix(ctx, callRoot+"/")
	if err != nil {
		return nil, err
	}
	sessions := make([]model.CallSession, 0, len(items))
	for _, item := range items {
		if !isMetaKey(item.Key) {
			continue
		}
		var call model.CallSession
		if err := etcdutil.Unmarshal(item.Value, &call); err != nil {
			return nil, err
		}
		if filter.TenantID != "" && call.TenantID != filter.TenantID {
			continue
		}
		if filter.State != "" && call.State != filter.State {
			continue
		}
		if filter.NodeID != "" && call.MediaNode != filter.NodeID && call.Owner.OwnerNode != filter.NodeID {
			continue
		}
		sessions = append(sessions, call)
	}
	// 按 callID 升序保证输出稳定。
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, nil
}

// MarkSuspect 将通话标记为"可疑"（节点健康检查失败时调用）。
func (m *Manager) MarkSuspect(ctx context.Context, callID string) error {
	state := model.CallStateSuspect
	call, err := m.GetSession(ctx, callID)
	if err != nil {
		return err
	}
	return m.UpdateSession(ctx, callID, call.Owner.Epoch, Update{State: &state})
}

// MarkRecovering 将通话标记为"恢复中"。
func (m *Manager) MarkRecovering(ctx context.Context, callID string) error {
	state := model.CallStateRecovering
	call, err := m.GetSession(ctx, callID)
	if err != nil {
		return err
	}
	return m.UpdateSession(ctx, callID, call.Owner.Epoch, Update{State: &state})
}

// MarkLost 将通话标记为"丢失"（最终状态，不可恢复）。
func (m *Manager) MarkLost(ctx context.Context, callID string) error {
	state := model.CallStateLost
	call, err := m.GetSession(ctx, callID)
	if err != nil {
		return err
	}
	return m.UpdateSession(ctx, callID, call.Owner.Epoch, Update{State: &state})
}

// getSessionItem 从 etcd 读取通话记录及其版本信息（供乐观更新使用）。
func (m *Manager) getSessionItem(ctx context.Context, callID string) (*model.CallSession, etcdutil.Item, error) {
	item, err := m.client.Get(ctx, metaKey(callID))
	if errors.Is(err, etcdutil.ErrKeyNotFound) {
		return nil, etcdutil.Item{}, pbxerrors.ErrCallNotFound
	}
	if err != nil {
		return nil, etcdutil.Item{}, err
	}
	var call model.CallSession
	if err := etcdutil.Unmarshal(item.Value, &call); err != nil {
		return nil, etcdutil.Item{}, err
	}
	return &call, item, nil
}

// get 从 etcd 读取 key 并反序列化到 target，key 不存在时返回 ErrCallNotFound。
func (m *Manager) get(ctx context.Context, key string, target any) error {
	item, err := m.client.Get(ctx, key)
	if errors.Is(err, etcdutil.ErrKeyNotFound) {
		return pbxerrors.ErrCallNotFound
	}
	if err != nil {
		return err
	}
	return etcdutil.Unmarshal(item.Value, target)
}

// metaKey 构建通话元数据的 etcd key：/pbx/calls/{callID}/meta。
func metaKey(callID string) string {
	return etcdutil.JoinKey(callRoot, callID, "meta")
}

// ownerKey 构建通话 owner 的 etcd key：/pbx/calls/{callID}/owner。
func ownerKey(callID string) string {
	return etcdutil.JoinKey(callRoot, callID, "owner")
}

// isMetaKey 判断 etcd key 是否以 /meta 结尾（用于过滤出通话元数据项）。
func isMetaKey(key string) bool {
	return len(key) >= 5 && key[len(key)-5:] == "/meta"
}

