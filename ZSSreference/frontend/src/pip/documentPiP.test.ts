import { describe, expect, it } from 'vitest'

import { isDocumentPiPSupported } from './documentPiP'

describe('isDocumentPiPSupported', () => {
  it('returns true when the target exposes documentPictureInPicture', () => {
    expect(
      isDocumentPiPSupported({
        documentPictureInPicture: {
          requestWindow: async () => window,
        },
      }),
    ).toBe(true)
  })

  it('returns false when the target does not expose documentPictureInPicture', () => {
    expect(isDocumentPiPSupported({})).toBe(false)
  })
})
