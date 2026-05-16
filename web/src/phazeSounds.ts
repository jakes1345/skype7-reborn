/** Base URL for static Phaze assets (sounds, emoticons) served by Nexus at `/public/phaze/assets/`. */
export function phazeAssetBase(): string {
  const fromEnv = import.meta.env.VITE_PHAZE_ASSET_BASE as string | undefined
  if (fromEnv?.trim()) {
    return fromEnv.replace(/\/$/, '')
  }
  const ws = import.meta.env.VITE_NEXUS_WS as string | undefined
  if (ws) {
    try {
      const u = new URL(ws.replace(/^ws/i, 'http'))
      return `${u.protocol}//${u.host}/public/phaze/assets`
    } catch {
      /* fall through */
    }
  }
  return '/public/phaze/assets'
}

/** Path under phaze assets root, e.g. `emoticons/emoticon_smile.png`. */
export function phazeEmoticonUrl(relativePath: string): string {
  const safe = relativePath.replace(/[^a-zA-Z0-9._/-]/g, '').replace(/^\/+/, '')
  if (!safe) return ''
  return `${phazeAssetBase()}/${safe}`
}

export function phazeSoundUrl(filename: string): string {
  const safe = filename.replace(/[^a-zA-Z0-9._-]/g, '')
  if (!safe) return ''
  return `${phazeAssetBase()}/sounds/${encodeURIComponent(safe)}`
}

let warnedMissing = false

/** Play a WAV from the Phaze asset tree (Nexus `/public/phaze/assets/sounds/`). No-op if URL is empty. */
export function playPhazeSound(filename: string): void {
  const url = phazeSoundUrl(filename)
  if (!url) return
  try {
    const a = new Audio(url)
    a.volume = 0.85
    void a.play().catch(() => {
      if (!warnedMissing && import.meta.env.DEV) {
        warnedMissing = true
        console.warn(
          '[Phaze] sound playback failed (is Nexus running and `make phaze-assets` done?):',
          url,
        )
      }
    })
  } catch {
    /* ignore */
  }
}
