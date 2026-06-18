import { describe, expect, it } from 'vitest'

import {
  isNearBottom,
  shouldAutoScrollOnUpdate,
  shouldPauseAutoScroll,
} from './subtitleScroll'

const thresholdPx = 24

describe('subtitleScroll', () => {
  it('treats the exact bottom as near bottom', () => {
    const metrics = { scrollTop: 700, clientHeight: 300, scrollHeight: 1000 }

    expect(isNearBottom(metrics, thresholdPx)).toBe(true)
    expect(shouldPauseAutoScroll(metrics, thresholdPx)).toBe(false)
  })

  it('keeps auto-scroll enabled within the bottom threshold', () => {
    const metrics = { scrollTop: 688, clientHeight: 300, scrollHeight: 1000 }

    expect(isNearBottom(metrics, thresholdPx)).toBe(true)
    expect(shouldPauseAutoScroll(metrics, thresholdPx)).toBe(false)
  })

  it('pauses auto-scroll above the bottom threshold', () => {
    const metrics = { scrollTop: 620, clientHeight: 300, scrollHeight: 1000 }

    expect(isNearBottom(metrics, thresholdPx)).toBe(false)
    expect(shouldPauseAutoScroll(metrics, thresholdPx)).toBe(true)
  })

  it('treats short content as near bottom', () => {
    const metrics = { scrollTop: 0, clientHeight: 600, scrollHeight: 400 }

    expect(isNearBottom(metrics, thresholdPx)).toBe(true)
    expect(shouldPauseAutoScroll(metrics, thresholdPx)).toBe(false)
  })

  it('keeps auto-scroll enabled at the threshold boundary', () => {
    const metrics = { scrollTop: 676, clientHeight: 300, scrollHeight: 1000 }

    expect(isNearBottom(metrics, thresholdPx)).toBe(true)
    expect(shouldPauseAutoScroll(metrics, thresholdPx)).toBe(false)
  })

  it('auto-scrolls updates when content overflows', () => {
    const metrics = { scrollTop: 120, clientHeight: 300, scrollHeight: 620 }

    expect(shouldAutoScrollOnUpdate(metrics)).toBe(true)
  })

  it('skips auto-scroll updates when content does not overflow', () => {
    expect(
      shouldAutoScrollOnUpdate({
        scrollTop: 0,
        clientHeight: 600,
        scrollHeight: 400,
      }),
    ).toBe(false)
    expect(shouldAutoScrollOnUpdate(null)).toBe(false)
  })
})
