const BAR_WEIGHTS = [0.34, 0.52, 0.76, 0.94, 0.68, 0.46, 0.82, 1, 0.74, 0.56, 0.9, 0.62, 0.42, 0.7, 0.86, 0.5]

type AudioVisualizerProps = {
  level: number
}

function clampLevel(level: number): number {
  if (!Number.isFinite(level)) {
    return 0
  }
  return Math.min(1, Math.max(0, level))
}

export function AudioVisualizer({ level }: AudioVisualizerProps) {
  const normalizedLevel = clampLevel(level)
  const meterLevel = `${Math.round(normalizedLevel * 100)}%`

  return (
    <div
      className="audio-visualizer"
      aria-label={`输入音量 ${meterLevel}`}
      data-active={normalizedLevel > 0 ? 'true' : 'false'}
    >
      <span>输入</span>
      <div className="audio-visualizer-bars" aria-hidden="true">
        {BAR_WEIGHTS.map((weight, index) => {
          const height = 18 + normalizedLevel * weight * 82
          return (
            <span
              key={`${index}-${weight}`}
              className="audio-visualizer-bar"
              style={{ height: `${height}%` }}
            />
          )
        })}
      </div>
      <span className="audio-visualizer-value">{meterLevel}</span>
    </div>
  )
}
