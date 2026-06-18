import { useEffect, useMemo, useState } from 'react'

import { documentPiP, isDocumentPiPSupported } from '../pip/documentPiP'
import { useSubtitleStore } from '../state/subtitles'
import {
  SubtitlePiPPanel,
  type SubtitlePiPControls,
  type SubtitlePiPSessionStatus,
} from './SubtitlePiPPanel'

type SubtitlePiPButtonProps = {
  sessionStatus: SubtitlePiPSessionStatus
  controls: SubtitlePiPControls
}

function getPiPLabel(isSupported: boolean, isOpen: boolean) {
  if (!isSupported) {
    return '悬浮窗不可用'
  }

  return isOpen ? '关闭悬浮字幕窗' : '打开悬浮字幕窗'
}

export function SubtitlePiPButton({ sessionStatus, controls }: SubtitlePiPButtonProps) {
  const lines = useSubtitleStore((state) => state.lines)
  const [isOpen, setIsOpen] = useState(documentPiP.isOpen())
  const [message, setMessage] = useState<string>()
  const isSupported = useMemo(() => isDocumentPiPSupported(), [])
  const panel = useMemo(
    () => (
      <SubtitlePiPPanel
        lines={lines}
        sessionStatus={sessionStatus}
        controls={controls}
      />
    ),
    [controls, lines, sessionStatus],
  )

  useEffect(() => documentPiP.subscribe(setIsOpen), [])

  useEffect(() => {
    if (!isOpen) {
      return
    }

    documentPiP.render(panel)
  }, [isOpen, panel])

  useEffect(
    () => () => {
      documentPiP.close()
    },
    [],
  )

  const handleClick = async () => {
    setMessage(undefined)

    if (isOpen) {
      documentPiP.close()
      return
    }

    try {
      await documentPiP.open(panel)
    } catch (error) {
      setMessage(
        error instanceof Error
          ? error.message
          : '悬浮字幕窗打开失败，请继续使用页面字幕面板。',
      )
    }
  }

  return (
    <div className="subtitle-pip-entry">
      <button
        className="secondary-action subtitle-pip-button"
        type="button"
        disabled={!isSupported}
        onClick={() => void handleClick()}
        title={
          isSupported
            ? undefined
            : '当前浏览器不支持悬浮字幕窗，请使用最新版 Chrome / Edge。'
        }
      >
        {getPiPLabel(isSupported, isOpen)}
      </button>
      {message ? (
        <p className="subtitle-pip-message" role="status">
          {message}
        </p>
      ) : null}
    </div>
  )
}
