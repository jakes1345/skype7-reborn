import { describe, expect, it } from 'vitest'
import {
  decryptFromPeer,
  decodePublicKeyField,
  encodePublicKeyB64,
  encryptForPeer,
  generateKeyPair,
} from './e2ee'

describe('e2ee', () => {
  it('roundtrips NaCl box between two keypairs', () => {
    const alice = generateKeyPair()
    const bob = generateKeyPair()
    const cipher = encryptForPeer('hello-μniverse', bob.publicKey, alice.secretKey)
    expect(cipher.startsWith('E2EE:')).toBe(true)
    const plain = decryptFromPeer(cipher, alice.publicKey, bob.secretKey)
    expect(plain).toBe('hello-μniverse')
  })

  it('decryptFromPeer leaves plaintext unchanged', () => {
    const alice = generateKeyPair()
    const bob = generateKeyPair()
    expect(decryptFromPeer('plain', alice.publicKey, bob.secretKey)).toBe('plain')
  })

  it('decodePublicKeyField accepts base64 from encodePublicKeyB64', () => {
    const { publicKey } = generateKeyPair()
    const b64 = encodePublicKeyB64(publicKey)
    const decoded = decodePublicKeyField(b64)
    expect(decoded).not.toBeNull()
    expect(decoded!.length).toBe(32)
    expect(Array.from(decoded!)).toEqual(Array.from(publicKey))
  })

  it('decodePublicKeyField rejects bad inputs', () => {
    expect(decodePublicKeyField(undefined)).toBeNull()
    expect(decodePublicKeyField('not-valid-base64!!!')).toBeNull()
    expect(decodePublicKeyField([])).toBeNull()
    expect(decodePublicKeyField(Array.from({ length: 31 }, (_, i) => i))).toBeNull()
  })
})
