// Package ai 是 AI Provider 层：封装 PBX 节点所需的全部 AI 能力接入，包括语音活动检测(VAD)、
// 语音识别(ASR)、机器翻译(TMT/TMT)、语音合成(TTS)以及大语言模型(LLM)翻译。
// 每种能力通过统一接口抽象，具体 provider 通过 init()+Register 模式注册，支持 mock/腾讯云/第三方。
package ai
