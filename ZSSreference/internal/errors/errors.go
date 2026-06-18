//  公共错误类型定义
package errors

import "errors"

var (
	ErrNodeNotFound        = errors.New("node not found")
	ErrLeaseExpired        = errors.New("lease expired")
	ErrCallNotFound        = errors.New("call not found")
	ErrEpochMismatch       = errors.New("epoch mismatch")
	ErrNoAvailableNode     = errors.New("no available node")
	ErrTenantDenied        = errors.New("tenant denied")
	ErrProviderUnavailable = errors.New("provider unavailable")
	ErrConflict            = errors.New("conflict")
	ErrNotFound            = errors.New("not found")
)

