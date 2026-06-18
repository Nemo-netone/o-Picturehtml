// ID生成器：雪花算法+节点ID+通话ID
package idgen

import (
	"fmt"
	"sync/atomic"
	"time"
)

type Prefix string

const (
	PrefixTenant    Prefix = "tenant"
	PrefixUser      Prefix = "user"
	PrefixExtension Prefix = "ext"
	PrefixTrunk     Prefix = "trunk"
	PrefixRoute     Prefix = "route"
	PrefixCall      Prefix = "call"
	PrefixSession   Prefix = "session"
	PrefixUtterance Prefix = "utt"
	PrefixRecording Prefix = "rec"
	PrefixEvent     Prefix = "evt"
	PrefixNode      Prefix = "node"
	PrefixWorker    Prefix = "worker"
)

type Generator struct {
	prefix  Prefix
	counter atomic.Uint64
}

// NewGenerator 创建 ID 生成器（prefix-时间戳-序号 格式）。
func NewGenerator(prefix Prefix) *Generator {
	return &Generator{prefix: prefix}
}

// Next 生成下一个 ID。
func (g *Generator) Next() string {
	sequence := g.counter.Add(1)
	return fmt.Sprintf("%s-%d-%06d", g.prefix, time.Now().UnixMilli(), sequence)
}

var (
	tenantGenerator    = NewGenerator(PrefixTenant)
	userGenerator      = NewGenerator(PrefixUser)
	extensionGenerator = NewGenerator(PrefixExtension)
	trunkGenerator     = NewGenerator(PrefixTrunk)
	routeGenerator     = NewGenerator(PrefixRoute)
	callGenerator      = NewGenerator(PrefixCall)
	sessionGenerator   = NewGenerator(PrefixSession)
	utteranceGenerator = NewGenerator(PrefixUtterance)
	recordingGenerator = NewGenerator(PrefixRecording)
	eventGenerator     = NewGenerator(PrefixEvent)
	nodeGenerator      = NewGenerator(PrefixNode)
	workerGenerator    = NewGenerator(PrefixWorker)
)

// TenantID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func TenantID() string {
	return tenantGenerator.Next()
}

// UserID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func UserID() string {
	return userGenerator.Next()
}

// ExtensionID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func ExtensionID() string {
	return extensionGenerator.Next()
}

// TrunkID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func TrunkID() string {
	return trunkGenerator.Next()
}

// RouteID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func RouteID() string {
	return routeGenerator.Next()
}

// CallID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func CallID() string {
	return callGenerator.Next()
}

// SessionID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func SessionID() string {
	return sessionGenerator.Next()
}

// UtteranceID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func UtteranceID() string {
	return utteranceGenerator.Next()
}

// RecordingID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func RecordingID() string {
	return recordingGenerator.Next()
}

// EventID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func EventID() string {
	return eventGenerator.Next()
}

// NodeID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func NodeID() string {
	return nodeGenerator.Next()
}

// WorkerID 是全局便捷函数，返回 prefix-时间戳-序号 格式的 ID。
func WorkerID() string {
	return workerGenerator.Next()
}
