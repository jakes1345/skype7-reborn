const PINS_KEY = 'phaze_key_pins_v1'

export type PinRow = { fingerprint: string; publicKeyB64: string }

export function loadPins(): Record<string, PinRow> {
  try {
    const raw = localStorage.getItem(PINS_KEY)
    if (!raw) return {}
    const o = JSON.parse(raw) as Record<string, PinRow>
    return o && typeof o === 'object' ? o : {}
  } catch {
    return {}
  }
}

export function savePins(p: Record<string, PinRow>) {
  localStorage.setItem(PINS_KEY, JSON.stringify(p))
}
