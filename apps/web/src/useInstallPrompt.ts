import { useSyncExternalStore } from 'react'

type BeforeInstallPromptEvent = Event & {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export type InstallPlatform = 'ios' | 'android' | 'desktop' | 'other'

type InstallSnapshot = {
  canInstall: boolean
  installed: boolean
  platform: InstallPlatform
}

export type InstallState = InstallSnapshot & {
  install: () => Promise<'accepted' | 'dismissed' | 'unavailable'>
}

function detectPlatform(): InstallPlatform {
  const ua = navigator.userAgent
  const iOS = /iPad|iPhone|iPod/.test(ua) || (ua.includes('Macintosh') && 'ontouchend' in document)
  if (iOS) return 'ios'
  if (/Android/.test(ua)) return 'android'
  if (/Chrome|Chromium|Edg\//.test(ua)) return 'desktop'
  return 'other'
}

function isStandalone() {
  return (
    window.matchMedia?.('(display-mode: standalone)').matches ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true
  )
}

let deferredPrompt: BeforeInstallPromptEvent | null = null
let snapshot: InstallSnapshot = {
  canInstall: false,
  installed: typeof window !== 'undefined' ? isStandalone() : false,
  platform: typeof window !== 'undefined' ? detectPlatform() : 'other',
}
const listeners = new Set<() => void>()

function update(partial: Partial<InstallSnapshot>) {
  snapshot = { ...snapshot, ...partial }
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function getSnapshot() {
  return snapshot
}

if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (event) => {
    event.preventDefault()
    deferredPrompt = event as BeforeInstallPromptEvent
    update({ canInstall: true })
  })
  window.addEventListener('appinstalled', () => {
    deferredPrompt = null
    update({ canInstall: false, installed: true })
  })
}

async function install(): Promise<'accepted' | 'dismissed' | 'unavailable'> {
  if (!deferredPrompt) return 'unavailable'
  await deferredPrompt.prompt()
  const { outcome } = await deferredPrompt.userChoice
  deferredPrompt = null
  update({ canInstall: false })
  return outcome
}

export function useInstallPrompt(): InstallState {
  const state = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
  return { ...state, install }
}
