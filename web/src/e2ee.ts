import nacl from 'tweetnacl'

const E2EE_PREFIX = 'E2EE:'

function toHex(u8: Uint8Array): string {
  return Array.from(u8, (b) => b.toString(16).padStart(2, '0')).join('')
}

function fromHex(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2)
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16)
  }
  return out
}

/** First 16 hex chars of SHA-256(pub), matching Go crypto.Fingerprint */
export async function fingerprint(pub: Uint8Array): Promise<string> {
  const h = await crypto.subtle.digest('SHA-256', pub.buffer.slice(pub.byteOffset, pub.byteOffset + pub.byteLength) as ArrayBuffer)
  const b = new Uint8Array(h).slice(0, 8)
  return toHex(b)
}

export function generateKeyPair(): { publicKey: Uint8Array; secretKey: Uint8Array } {
  return nacl.box.keyPair()
}

export function encryptForPeer(
  plain: string,
  peerPub: Uint8Array,
  mySecret: Uint8Array,
): string {
  const msg = new TextEncoder().encode(plain)
  const nonce = nacl.randomBytes(24)
  const boxed = nacl.box(msg, nonce, peerPub, mySecret)
  const combined = new Uint8Array(24 + boxed.length)
  combined.set(nonce, 0)
  combined.set(boxed, 24)
  return E2EE_PREFIX + toHex(combined)
}

export function decryptFromPeer(field: string, senderPub: Uint8Array, mySecret: Uint8Array): string {
  if (!field.startsWith(E2EE_PREFIX)) return field
  const raw = fromHex(field.slice(E2EE_PREFIX.length))
  if (raw.length < 24) return ''
  const nonce = raw.slice(0, 24)
  const boxed = raw.slice(24)
  const opened = nacl.box.open(boxed, nonce, senderPub, mySecret)
  if (!opened) return ''
  return new TextDecoder().decode(opened)
}

/** Decode public_key from Nexus JSON (base64 string or number[]). */
export function decodePublicKeyField(v: string | number[] | undefined): Uint8Array | null {
  if (v == null) return null
  if (typeof v === 'string') {
    let bin: string
    try {
      bin = atob(v)
    } catch {
      return null
    }
    const out = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
    return out.length === 32 ? out : null
  }
  if (Array.isArray(v) && v.length === 32) {
    return new Uint8Array(v)
  }
  return null
}

export function encodePublicKeyB64(pub: Uint8Array): string {
  let s = ''
  for (let i = 0; i < pub.length; i++) s += String.fromCharCode(pub[i])
  return btoa(s)
}
