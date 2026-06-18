export type ScrollMetrics = {
  scrollTop: number
  scrollHeight: number
  clientHeight: number
}

export function isNearBottom(
  { scrollTop, scrollHeight, clientHeight }: ScrollMetrics,
  thresholdPx: number,
) {
  return scrollHeight - scrollTop - clientHeight <= thresholdPx
}

export function shouldPauseAutoScroll(
  metrics: ScrollMetrics,
  thresholdPx: number,
) {
  return !isNearBottom(metrics, thresholdPx)
}

export function shouldAutoScrollOnUpdate(target: ScrollMetrics | null | undefined) {
  if (!target) {
    return false
  }

  return target.scrollHeight > target.clientHeight
}
