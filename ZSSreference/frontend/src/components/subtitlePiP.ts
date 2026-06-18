import type { SubtitleLine } from '../types/protocol'

export const PIP_MAX_VISIBLE_LINES = 8

export function getVisiblePiPLines(
  lines: SubtitleLine[],
  maxLines = PIP_MAX_VISIBLE_LINES,
) {
  if (maxLines <= 0) {
    return []
  }

  return lines.filter((line) => line.en || line.zh).slice(-maxLines)
}

export function getPiPScrollKey(lines: SubtitleLine[]) {
  if (lines.length === 0) {
    return 'empty'
  }

  return lines
    .map((line) =>
      [
        line.utteranceId,
        line.en,
        line.zh,
        String(line.enFinal),
        String(line.zhFinal),
        String(line.revised),
      ].join('|'),
    )
    .join('||')
}
