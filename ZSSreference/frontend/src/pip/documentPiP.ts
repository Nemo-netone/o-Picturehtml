import { createRoot, type Root } from 'react-dom/client'
import type { ReactNode } from 'react'

import { PIP_WINDOW_HEIGHT, PIP_WINDOW_WIDTH } from '../config'
import { SUBTITLE_COLOR, SUBTITLE_MOTION } from '../styles/tokens'

type DocumentPictureInPictureController = {
  requestWindow: (options: { width: number; height: number }) => Promise<Window>
}

type DocumentPiPWindow = Window & typeof globalThis

declare global {
  interface Window {
    documentPictureInPicture?: DocumentPictureInPictureController
  }
}

type PiPListener = (isOpen: boolean) => void

const PIP_UNSUPPORTED_MESSAGE =
  '当前浏览器不支持悬浮字幕窗，请使用最新版 Chrome / Edge，或继续使用页面字幕面板。'

function createSubtitlePiPStyles() {
  return `
    :root {
      --surface: #f7f8f4;
      --panel: #ffffff;
      --text: #2f3439;
      --text-strong: #101418;
      --text-muted: #66707a;
      --border: #d6dbe1;
      --border-strong: #15191d;
      --brand: #2563eb;
      --brand-bg: #eaf1ff;
      --success: #159447;
      --warning: #d59b00;
      --danger: #d33f49;
      --pixel-shadow: #15191d;
      --pixel-grid: #e7ebef;
      --subtitle-pending: ${SUBTITLE_COLOR.pending};
      --subtitle-final: ${SUBTITLE_COLOR.final};
      --subtitle-revise-highlight: ${SUBTITLE_COLOR.reviseHighlight};
      --revise-highlight-ms: ${SUBTITLE_MOTION.reviseHighlightMs}ms;
      font: 16px/1.5 ui-monospace, 'SFMono-Regular', 'Cascadia Mono', 'Liberation Mono', 'Menlo', 'PingFang SC', 'Microsoft YaHei', 'Noto Sans CJK SC', system-ui, monospace;
      color: var(--text);
      background: var(--surface);
      color-scheme: light;
      -webkit-font-smoothing: none;
    }

    * {
      box-sizing: border-box;
      letter-spacing: 0;
    }

    body {
      margin: 0;
      min-width: 0;
      min-height: 100vh;
      overflow: hidden;
      background:
        linear-gradient(90deg, var(--pixel-grid) 1px, transparent 1px),
        linear-gradient(180deg, var(--pixel-grid) 1px, transparent 1px),
        var(--surface);
      background-size: 14px 14px;
    }

    #subtitle-pip-root {
      height: 100vh;
    }

    .subtitle-pip {
      display: flex;
      height: 100vh;
      min-height: 0;
      flex-direction: column;
      gap: 18px;
      padding: 28px;
      border: 0;
      border-radius: 0;
      background:
        linear-gradient(90deg, rgba(21, 25, 29, 0.035) 1px, transparent 1px),
        linear-gradient(180deg, rgba(21, 25, 29, 0.035) 1px, transparent 1px),
        var(--panel);
      background-size: 12px 12px;
      box-shadow: none;
    }

    .subtitle-pip-body {
      min-height: 0;
      flex: 1;
      overflow-y: auto;
      overscroll-behavior: contain;
      padding: 6px;
      scrollbar-gutter: stable;
    }

    .subtitle-pip-body::-webkit-scrollbar {
      width: 8px;
    }

    .subtitle-pip-body::-webkit-scrollbar-thumb {
      border: 2px solid var(--panel);
      border-radius: 1px;
      background: var(--border-strong);
      background-clip: padding-box;
    }

    .subtitle-pip-body::-webkit-scrollbar-track {
      background: transparent;
    }

    .subtitle-pip-footer {
      display: flex;
      flex: 0 0 auto;
      align-items: center;
      justify-content: center;
      gap: 9px;
      min-width: 0;
      overflow: visible;
      white-space: nowrap;
    }

    .subtitle-pip-footer-main {
      display: inline-flex;
      min-width: 0;
      flex: 0 1 auto;
      align-items: center;
      justify-content: center;
      gap: 8px;
    }

    .subtitle-pip-chip {
      display: inline-flex;
      height: 36px;
      flex: 0 0 auto;
      align-items: center;
      gap: 7px;
      border: 1px solid rgba(234, 223, 212, 0.76);
      border-radius: 4px;
      padding: 0 12px;
      background: rgba(255, 248, 237, 0.76);
      color: var(--text-muted);
      font-size: 13px;
      font-weight: 800;
      white-space: nowrap;
      box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.72);
    }

    .subtitle-pip-field {
      position: relative;
      min-width: 0;
      width: 116px;
    }

    .subtitle-pip-field .custom-select {
      position: relative;
      width: 100%;
    }

    .subtitle-pip-field .custom-select-trigger {
      display: inline-flex;
      width: 100%;
      height: 36px;
      min-width: 0;
      align-items: center;
      justify-content: space-between;
      gap: 7px;
      border: 1px solid rgba(234, 223, 212, 0.76);
      border-radius: 4px;
      padding: 0 10px 0 12px;
      background: rgba(255, 248, 237, 0.76);
      color: var(--text-strong);
      box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.72);
      font: inherit;
      font-size: 13px;
      font-weight: 850;
      line-height: 1;
      cursor: pointer;
      white-space: nowrap;
    }

    .subtitle-pip-field .custom-select-trigger span:first-child {
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .subtitle-pip-field .custom-select-trigger:disabled {
      cursor: not-allowed;
      opacity: 0.55;
    }

    .subtitle-pip-field .custom-select-trigger:focus-visible {
      border-color: rgba(216, 149, 84, 0.58);
      box-shadow:
        inset 0 1px 0 rgba(255, 255, 255, 0.72),
        0 0 0 3px rgba(216, 149, 84, 0.14);
      outline: none;
    }

    .subtitle-pip-field .custom-select-chevron {
      width: 7px;
      height: 7px;
      flex: 0 0 auto;
      border-right: 2px solid currentColor;
      border-bottom: 2px solid currentColor;
      color: var(--text-muted);
      transform: translateY(-2px) rotate(45deg);
      transition: transform 140ms ease;
    }

    .subtitle-pip-field .custom-select[data-open='true'] .custom-select-chevron {
      transform: translateY(2px) rotate(225deg);
    }

    .subtitle-pip-field .custom-select-menu {
      position: absolute;
      right: 0;
      bottom: calc(100% + 8px);
      left: 0;
      z-index: 10;
      overflow: hidden;
      padding: 5px;
      border: 1px solid rgba(234, 223, 212, 0.86);
      border-radius: 6px;
      background: var(--panel);
      box-shadow: 0 16px 32px rgba(107, 79, 56, 0.16);
    }

    .subtitle-pip-field .custom-select-option {
      display: flex;
      width: 100%;
      height: 34px;
      min-width: 0;
      align-items: center;
      justify-content: flex-start;
      border: 0;
      border-radius: 4px;
      padding: 0 9px;
      background: transparent;
      color: var(--text-strong);
      font: inherit;
      font-size: 13px;
      font-weight: 750;
      cursor: pointer;
      white-space: nowrap;
    }

    .subtitle-pip-field .custom-select-option:hover,
    .subtitle-pip-field .custom-select-option:focus-visible {
      background: #fff9f1;
      color: #9f6330;
      outline: none;
    }

    .subtitle-pip-field .custom-select-option[aria-selected='true'] {
      background: var(--brand-bg);
      color: #9f6330;
      font-weight: 850;
    }

    .subtitle-pip-switch-field input:disabled,
    .subtitle-pip-footer-action:disabled {
      cursor: not-allowed;
      opacity: 0.55;
    }

    .subtitle-pip-connection-status {
      height: auto;
      gap: 7px;
      border: 0;
      padding: 0 2px;
      background: transparent;
      box-shadow: none;
      color: var(--text-muted);
    }

    .subtitle-pip-connection-status::before {
      display: block;
      width: 7px;
      height: 7px;
      border-radius: 4px;
      background: var(--warning);
      box-shadow: 0 0 0 4px rgba(216, 149, 84, 0.13);
      content: '';
    }

    .subtitle-pip-connection-status-connected::before {
      background: var(--success);
      box-shadow: 0 0 0 4px rgba(79, 155, 107, 0.13);
    }

    .subtitle-pip-connection-status-disconnected::before {
      background: var(--danger);
      box-shadow: 0 0 0 4px rgba(201, 105, 85, 0.13);
    }

    .subtitle-pip-switch-field {
      position: relative;
      display: inline-flex;
      height: 36px;
      flex: 0 0 auto;
      align-items: center;
      gap: 6px;
      color: var(--text-muted);
      font-size: 13px;
      font-weight: 800;
      white-space: nowrap;
      user-select: none;
      -webkit-tap-highlight-color: transparent;
    }

    .subtitle-pip-switch-field input {
      position: absolute;
      inset: 0;
      width: 100%;
      height: 100%;
      margin: 0;
      border: 0;
      background: transparent;
      cursor: inherit;
      opacity: 0;
      outline: none;
      appearance: none;
      -webkit-appearance: none;
      -webkit-tap-highlight-color: transparent;
    }

    .subtitle-pip-switch-field input:focus,
    .subtitle-pip-switch-field input:focus-visible {
      outline: none;
    }

    .subtitle-pip-switch-indicator {
      display: grid;
      width: 17px;
      height: 17px;
      flex: 0 0 auto;
      place-items: center;
      border: 2px solid #c9c2ba;
      border-radius: 4px;
      background: rgba(255, 255, 255, 0.72);
    }

    .subtitle-pip-switch-field[data-enabled='true'] .subtitle-pip-switch-indicator {
      border-color: var(--brand);
      background: var(--brand);
    }

    .subtitle-pip-switch-field[data-enabled='true'] .subtitle-pip-switch-indicator::after {
      width: 5px;
      height: 10px;
      border-right: 3px solid #2f3439;
      border-bottom: 3px solid #2f3439;
      transform: translateY(-1px) rotate(45deg);
      content: '';
    }

    .subtitle-pip-footer-action {
      min-width: 54px;
      justify-content: center;
      border-color: rgba(216, 149, 84, 0.42);
      background: var(--brand-bg);
      color: #9f6330;
      font: inherit;
      font-size: 13px;
      font-weight: 900;
      cursor: pointer;
      transition:
        transform 140ms ease,
        box-shadow 140ms ease;
    }

    .subtitle-pip-footer-action-stop {
      border-color: rgba(201, 105, 85, 0.34);
      background: #fff1ed;
      color: var(--danger);
    }

    .subtitle-pip-footer-action:not(:disabled):hover {
      transform: translateY(-1px);
      box-shadow:
        inset 0 1px 0 rgba(255, 255, 255, 0.72),
        0 8px 18px rgba(107, 79, 56, 0.08);
    }

    .subtitle-pip-control-message {
      min-width: 0;
      margin: 0;
      overflow: hidden;
      color: var(--text-muted);
      font-size: 12px;
      font-weight: 750;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .subtitle-pip-control-message-error {
      color: var(--danger);
    }

    .subtitle-pip-list {
      display: flex;
      min-height: 100%;
      min-width: 0;
      flex-direction: column;
      justify-content: flex-end;
      gap: 10px;
      padding-right: 2px;
    }

    .subtitle-pip-card {
      display: flex;
      min-width: 0;
      flex: 0 0 auto;
      flex-direction: column;
      gap: 10px;
      padding: 14px;
      border: 1px solid rgba(234, 223, 212, 0.92);
      border-radius: 6px;
      background: rgba(255, 255, 252, 0.94);
      box-shadow: 0 8px 20px rgba(107, 79, 56, 0.055);
    }

    .subtitle-pip-card-revised {
      animation: subtitle-pip-revise-highlight var(--revise-highlight-ms) ease-out;
    }

    .subtitle-pip-card-current {
      border-color: rgba(216, 149, 84, 0.42);
      box-shadow:
        inset 3px 0 0 var(--brand),
        0 8px 20px rgba(107, 79, 56, 0.07);
    }

    .subtitle-pip-row {
      display: grid;
      grid-template-columns: 34px minmax(0, 1fr) auto;
      align-items: baseline;
      gap: 10px;
    }

    .subtitle-pip-language {
      color: #87776a;
      font-size: 11px;
      font-weight: 900;
      letter-spacing: 0.04em;
    }

    .subtitle-pip-text {
      margin: 0;
      overflow-wrap: anywhere;
      font-size: 15px;
      line-height: 1.55;
    }

    .subtitle-pip-row-zh .subtitle-pip-text {
      font-size: 17px;
      font-weight: 650;
    }

    .subtitle-pip-badges {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      white-space: nowrap;
    }

    .subtitle-pip-lock {
      color: #d6a56d;
      font-size: 12px;
    }

    .subtitle-pip-revised {
      padding: 3px 8px;
      border-radius: 4px;
      background: var(--brand-bg);
      color: #9f6330;
      font-size: 11px;
      font-weight: 800;
    }

    .subtitle-pip-empty {
      display: flex;
      min-height: 100%;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 10px;
      padding: 24px;
      text-align: center;
    }

    .subtitle-pip-empty-title {
      margin: 0;
      color: var(--text-strong);
      font-size: 24px;
      font-weight: 850;
      line-height: 1.2;
    }

    .subtitle-pip-empty-hint {
      margin: 0;
      max-width: 360px;
      color: var(--text-muted);
      font-size: 15px;
      font-weight: 650;
      line-height: 1.65;
    }

    .subtitle-pip-chip,
    .subtitle-pip-field .custom-select-trigger,
    .subtitle-pip-field .custom-select-menu,
    .subtitle-pip-card {
      border: 2px solid var(--border-strong);
      border-radius: 3px;
      background: var(--panel);
      box-shadow: 2px 2px 0 var(--pixel-shadow);
    }

    .subtitle-pip-field .custom-select-trigger:focus-visible,
    .subtitle-pip-footer-action:focus-visible,
    .subtitle-pip-field .custom-select-option:focus-visible {
      border-color: var(--brand);
      box-shadow:
        2px 2px 0 var(--pixel-shadow),
        0 0 0 2px #bfd2ff;
      outline: none;
    }

    .subtitle-pip-field .custom-select-option {
      border-radius: 2px;
    }

    .subtitle-pip-field .custom-select-option:hover,
    .subtitle-pip-field .custom-select-option:focus-visible {
      background: #f1f5ff;
      color: var(--brand);
    }

    .subtitle-pip-field .custom-select-option[aria-selected='true'],
    .subtitle-pip-revised {
      background: var(--brand-bg);
      color: var(--brand);
      box-shadow: inset 0 -2px 0 rgba(37, 99, 235, 0.22);
    }

    .subtitle-pip-switch-indicator {
      border-color: var(--border-strong);
      border-radius: 2px;
      background: var(--panel);
    }

    .subtitle-pip-switch-field[data-enabled='true'] .subtitle-pip-switch-indicator {
      border-color: var(--border-strong);
      background: var(--brand);
    }

    .subtitle-pip-switch-field[data-enabled='true'] .subtitle-pip-switch-indicator::after {
      border-color: #ffffff;
    }

    .subtitle-pip-footer-action {
      border: 2px solid var(--border-strong);
      border-radius: 2px;
      background: var(--brand);
      color: #ffffff;
      box-shadow: 2px 2px 0 var(--pixel-shadow);
      transition: none;
    }

    .subtitle-pip-footer-action-stop {
      background: var(--panel);
      color: var(--danger);
    }

    .subtitle-pip-footer-action:not(:disabled):hover {
      transform: none;
      box-shadow: 2px 2px 0 var(--pixel-shadow);
    }

    .subtitle-pip-footer-action:not(:disabled):active {
      transform: translate(2px, 2px);
      box-shadow: 0 0 0 var(--pixel-shadow);
    }

    .subtitle-pip-connection-status::before {
      border-radius: 1px;
      box-shadow: 0 0 0 2px #fff3c4;
    }

    .subtitle-pip-connection-status-connected::before {
      box-shadow: 0 0 0 2px #cbf3d9;
    }

    .subtitle-pip-connection-status-disconnected::before {
      box-shadow: 0 0 0 2px #ffd4d8;
    }

    .subtitle-pip-card {
      background:
        linear-gradient(90deg, rgba(21, 25, 29, 0.03) 1px, transparent 1px),
        linear-gradient(180deg, rgba(21, 25, 29, 0.03) 1px, transparent 1px),
        var(--panel);
      background-size: 10px 10px;
    }

    .subtitle-pip-card-current {
      border-color: var(--brand);
      box-shadow:
        inset 4px 0 0 var(--brand),
        2px 2px 0 var(--pixel-shadow);
    }

    .subtitle-pip-language {
      color: var(--text-muted);
    }

    .subtitle-pip-lock {
      color: var(--warning);
    }

    @keyframes subtitle-pip-revise-highlight {
      0% { background: var(--subtitle-revise-highlight); }
      100% { background: var(--panel); }
    }
  `
}

export function isDocumentPiPSupported(
  target?: Partial<Window>,
): target is Window & {
  documentPictureInPicture: DocumentPictureInPictureController
} {
  if (target) {
    return 'documentPictureInPicture' in target
  }

  return typeof window !== 'undefined' && 'documentPictureInPicture' in window
}

class DocumentPiPManager {
  private pipWindow?: DocumentPiPWindow
  private root?: Root
  private container?: HTMLDivElement
  private listeners = new Set<PiPListener>()
  private cleanupPagehide?: () => void

  subscribe(listener: PiPListener) {
    this.listeners.add(listener)
    listener(this.isOpen())

    return () => {
      this.listeners.delete(listener)
    }
  }

  isOpen() {
    return Boolean(this.pipWindow && !this.pipWindow.closed && this.root)
  }

  async open(node: ReactNode) {
    if (!isDocumentPiPSupported()) {
      throw new Error(PIP_UNSUPPORTED_MESSAGE)
    }

    if (this.isOpen()) {
      this.render(node)
      return
    }

    const controller = window.documentPictureInPicture
    if (!controller) {
      throw new Error(PIP_UNSUPPORTED_MESSAGE)
    }

    const pipWindow = (await controller.requestWindow({
      width: PIP_WINDOW_WIDTH,
      height: PIP_WINDOW_HEIGHT,
    })) as DocumentPiPWindow

    this.pipWindow = pipWindow
    this.initializeDocument(pipWindow)
    this.render(node)
    this.notify()
  }

  render(node: ReactNode) {
    if (!this.root) {
      return
    }

    this.root.render(node)
  }

  close() {
    const pipWindow = this.pipWindow
    this.cleanup()

    if (pipWindow && !pipWindow.closed) {
      pipWindow.close()
    }
  }

  private initializeDocument(pipWindow: DocumentPiPWindow) {
    const pipDocument = pipWindow.document
    pipDocument.title = 'SimulSpeak 字幕'

    const style = pipDocument.createElement('style')
    style.textContent = createSubtitlePiPStyles()
    pipDocument.head.appendChild(style)

    const container = pipDocument.createElement('div')
    container.id = 'subtitle-pip-root'
    pipDocument.body.appendChild(container)

    this.container = container
    this.root = createRoot(container)

    const handlePagehide = () => {
      this.cleanup()
    }
    pipWindow.addEventListener('pagehide', handlePagehide)
    this.cleanupPagehide = () => {
      pipWindow.removeEventListener('pagehide', handlePagehide)
    }
  }

  private cleanup() {
    this.cleanupPagehide?.()
    this.cleanupPagehide = undefined
    this.root?.unmount()
    this.root = undefined
    this.container?.remove()
    this.container = undefined
    this.pipWindow = undefined
    this.notify()
  }

  private notify() {
    const isOpen = this.isOpen()
    for (const listener of this.listeners) {
      listener(isOpen)
    }
  }
}

export const documentPiP = new DocumentPiPManager()
export { PIP_UNSUPPORTED_MESSAGE }
