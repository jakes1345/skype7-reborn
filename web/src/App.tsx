import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, startTransition } from 'react'
import type { NexusMessage, TurnConfig } from './nexusTypes'
import {
  decryptFromPeer,
  decodePublicKeyField,
  encryptForPeer,
  encodePublicKeyB64,
  fingerprint,
  generateKeyPair,
} from './e2ee'
import { loadPins, savePins } from './keyPins'
import { playPhazeSound } from './phazeSounds'
import Spaces from './Spaces'
import './App.css'

const SESSION_KEY = 'phaze_session_token_v1'
const KEYS_KEY = 'phaze_nacl_keys_v1'

type ChatLine = { id: string; from: string; text: string; me: boolean }

function defaultWsUrl(): string {
  const u = import.meta.env.VITE_NEXUS_WS as string | undefined
  if (u) return u
  const { protocol, hostname, port } = window.location
  const p = protocol === 'https:' ? 'wss:' : 'ws:'
  const h = port && protocol !== 'https:' ? `${hostname}:${port}` : hostname
  return `${p}//${h}/ws`
}

function loadOrCreateKeys(): { publicKey: Uint8Array; secretKey: Uint8Array } {
  try {
    const raw = localStorage.getItem(KEYS_KEY)
    if (raw) {
      const j = JSON.parse(raw) as { pub: string; sec: string }
      const pub = Uint8Array.from(atob(j.pub), (c) => c.charCodeAt(0))
      const sec = Uint8Array.from(atob(j.sec), (c) => c.charCodeAt(0))
      if (pub.length === 32 && sec.length === 32) return { publicKey: pub, secretKey: sec }
    }
  } catch {
    /* fallthrough */
  }
  const kp = generateKeyPair()
  localStorage.setItem(
    KEYS_KEY,
    JSON.stringify({
      pub: btoa(String.fromCharCode(...kp.publicKey)),
      sec: btoa(String.fromCharCode(...kp.secretKey)),
    }),
  )
  return kp
}

export default function App() {
  const wsUrl = useMemo(() => defaultWsUrl(), [])
  const [conn, setConn] = useState<'off' | 'connecting' | 'open'>('off')
  const [me, setMe] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const [log, setLog] = useState<ChatLine[]>([])
  const [friends, setFriends] = useState<Record<string, string>>({})
  const [selected, setSelected] = useState<string | null>(null)
  const [pending, setPending] = useState<string[]>([])
  const [draft, setDraft] = useState('')
  const [turn, setTurn] = useState<TurnConfig | null>(null)
  const [e2eReady, setE2eReady] = useState(false)

  // Auth + registration UI state. Declared up here (not lower with the
  // other view-only setters) because the WS message handler installed in
  // useLayoutEffect below references setMode/setRegStep, and ESLint's
  // no-use-before-define refuses lexically-later useState declarations.
  const [loginUser, setLoginUser] = useState('')
  const [loginPass, setLoginPass] = useState('')
  const [loginTotp, setLoginTotp] = useState('')
  const [addFriend, setAddFriend] = useState('')
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [regStep, setRegStep] = useState<'form' | 'verify' | 'done'>('form')
  const [regUser, setRegUser] = useState('')
  const [regEmail, setRegEmail] = useState('')
  const [regPass, setRegPass] = useState('')
  const [regCode, setRegCode] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteConfirmText, setDeleteConfirmText] = useState('')
  const [deletePassword, setDeletePassword] = useState('')

  // Top-level view mode: "dms" = the original direct-message UI;
  // "spaces" = the Discord-style servers + channels surface.
  const [view, setView] = useState<'dms' | 'spaces'>('dms')

  // Generic message-bus so child components (Spaces) can subscribe to
  // server-/channel-* messages without us hard-coding handlers per
  // component. Each handler runs after the main App reducer dispatches.
  const subscribersRef = useRef(new Set<(m: NexusMessage) => void>())
  const subscribe = useCallback((handler: (m: NexusMessage) => void) => {
    subscribersRef.current.add(handler)
    return () => {
      subscribersRef.current.delete(handler)
    }
  }, [])

  const wsRef = useRef<WebSocket | null>(null)
  const keysRef = useRef(loadOrCreateKeys())
  const peerKeysRef = useRef<Record<string, Uint8Array>>({})
  const pinsRef = useRef(loadPins())
  const meRef = useRef<string | null>(null)
  const selectedRef = useRef<string | null>(null)

  const sendRef = useRef<(m: NexusMessage) => void>(() => {})

  useEffect(() => {
    meRef.current = me
  }, [me])

  useEffect(() => {
    selectedRef.current = selected
  }, [selected])

  const appendLog = useCallback((from: string, text: string, isMe: boolean) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`
    setLog((prev) => [...prev, { id, from, text, me: isMe }])
  }, [])

  const acceptPeerKey = useCallback((peer: string, pk: Uint8Array, fpHint: string) => {
    void (async () => {
      const fp = await fingerprint(pk)
      if (fpHint && fpHint !== fp) {
        setErr(`Key fingerprint mismatch for ${peer}`)
        return
      }
      const prev = pinsRef.current[peer]
      if (prev && prev.fingerprint !== fp) {
        setErr(`Possible MITM: ${peer} key changed (pinned ${prev.fingerprint}, now ${fp})`)
        return
      }
      if (!prev) {
        pinsRef.current[peer] = { fingerprint: fp, publicKeyB64: encodePublicKeyB64(pk) }
        savePins(pinsRef.current)
      }
      peerKeysRef.current[peer] = pk
      if (peer === selectedRef.current) {
        setE2eReady(true)
      }
    })()
  }, [])

  const unwrap = useCallback((msg: NexusMessage): NexusMessage => {
    const sender = msg.sender ?? ''
    if (!sender) return msg
    const pk = peerKeysRef.current[sender]
    const sk = keysRef.current.secretKey
    const out = { ...msg }
    if (out.body && pk) out.body = decryptFromPeer(out.body, pk, sk)
    if (out.sdp && pk) out.sdp = decryptFromPeer(out.sdp, pk, sk)
    if (out.candidate && pk) out.candidate = decryptFromPeer(out.candidate, pk, sk)
    return out
  }, [])

  const onMessageRef = useRef<(raw: NexusMessage) => void>(() => {})

  useLayoutEffect(() => {
    sendRef.current = (m: NexusMessage) => {
      const w = wsRef.current
      if (!w || w.readyState !== WebSocket.OPEN) {
        setErr('Not connected')
        return
      }
      w.send(JSON.stringify(m))
    }
  })

  useLayoutEffect(() => {
    onMessageRef.current = (raw: NexusMessage) => {
      const msg = unwrap(raw)

      switch (msg.type) {
        case 'auth_result':
          if (msg.status === 'ok' && msg.qr_token) {
            localStorage.setItem(SESSION_KEY, msg.qr_token)
            setMe(msg.sender ?? null)
            setErr('')
            if (msg.turn_config) setTurn(msg.turn_config)
            playPhazeSound('Login.wav')
            sendRef.current({
              type: 'presence',
              sender: msg.sender,
              status: 'Online',
              public_key: encodePublicKeyB64(keysRef.current.publicKey),
            })
          } else {
            localStorage.removeItem(SESSION_KEY)
            if (msg.status === 'totp_required') setErr('2FA required: enter TOTP code.')
            else setErr(msg.error || msg.status || 'Auth failed')
          }
          break

        case 'friend_status':
          if (msg.sender) setFriends((f) => ({ ...f, [msg.sender!]: msg.status || 'Offline' }))
          break

        case 'pending_requests':
          if (msg.results?.length) setPending(msg.results)
          break

        case 'friend_request':
          if (msg.sender) setPending((p) => (p.includes(msg.sender!) ? p : [...p, msg.sender!]))
          break

        case 'friend_accepted':
          if (msg.sender) appendLog('system', `${msg.sender} accepted your friend request`, false)
          break

        case 'register_result':
          if (msg.status === 'ok' || msg.status === 'verification_sent') {
            setErr('Account created. Check your email for a 6-digit code, enter it below.')
            setRegStep('verify')
          } else {
            setErr(msg.error || 'Registration failed')
          }
          break

        case 'verify_result':
          if (msg.status === 'ok') {
            setErr('Email verified. You can sign in now.')
            setRegStep('done')
            setMode('login')
          } else {
            setErr(msg.error || 'Verification failed — double-check the code')
          }
          break

        case 'presence': {
          const pk = decodePublicKeyField(msg.public_key as string | number[] | undefined)
          if (msg.sender && pk && pk.length === 32) {
            acceptPeerKey(msg.sender, pk, msg.key_fingerprint || '')
          }
          if (msg.sender && msg.status) {
            setFriends((f) => ({ ...f, [msg.sender!]: msg.status! }))
          }
          break
        }

        case 'key_request':
          if (msg.sender) {
            const my = meRef.current
            if (my) {
              void fingerprint(keysRef.current.publicKey).then((fp) => {
                sendRef.current({
                  type: 'presence',
                  sender: my,
                  recipient: msg.sender,
                  status: 'Online',
                  public_key: encodePublicKeyB64(keysRef.current.publicKey),
                  key_fingerprint: fp,
                })
              })
            }
          }
          break

        case 'msg':
          if (msg.sender && msg.body !== undefined) {
            appendLog(msg.sender, msg.body || '[empty]', msg.sender === meRef.current)
            if (msg.sender !== meRef.current) {
              playPhazeSound('MessageReceived.wav')
            }
          }
          break

        case 'kicked':
          localStorage.removeItem(SESSION_KEY)
          setErr(msg.body || 'Kicked')
          setMe(null)
          break

        case 'delete_account_result':
          if (msg.status === 'ok') {
            // Server has erased the account and will close the socket.
            // Wipe local state so a return visit doesn't try to session-resume
            // against a now-deleted account.
            localStorage.removeItem(SESSION_KEY)
            localStorage.removeItem(KEYS_KEY)
            peerKeysRef.current = {}
            pinsRef.current = {}
            try {
              localStorage.removeItem('phaze_key_pins_v1')
            } catch {
              /* fine */
            }
            setMe(null)
            setFriends({})
            setPending([])
            setSelected(null)
            setLog([])
            setErr('Account deleted. All your data has been erased.')
          } else {
            setErr(msg.error || 'Delete failed')
          }
          break

        default:
          break
      }
      // Fan out to any subscribers (Spaces component listens for
      // server_*/channel_* types). We always fan out, even for handled
      // messages, so future surfaces can observe everything.
      subscribersRef.current.forEach((sub) => {
        try {
          sub(msg)
        } catch {
          /* swallow — one subscriber crashing must not poison the bus */
        }
      })
    }
  }, [unwrap, appendLog, acceptPeerKey])

  useEffect(() => {
    const w = new WebSocket(wsUrl)
    wsRef.current = w
    startTransition(() => {
      setConn('connecting')
      setErr('')
    })

    w.onopen = () => {
      setConn('open')
      const tok = localStorage.getItem(SESSION_KEY)
      const host = window.location.hostname
      if (tok) {
        sendRef.current({
          type: 'session_auth',
          qr_token: tok,
          device_info: `web/${host}`,
        })
      }
    }

    w.onmessage = (ev) => {
      try {
        const m = JSON.parse(ev.data as string) as NexusMessage
        onMessageRef.current(m)
      } catch {
        setErr('Bad message from server')
      }
    }

    w.onerror = () => setErr('WebSocket error')
    w.onclose = () => {
      setConn('off')
      wsRef.current = null
    }

    return () => {
      w.close()
      wsRef.current = null
    }
  }, [wsUrl])

  const send = useCallback((m: NexusMessage) => {
    sendRef.current(m)
  }, [])

  const doAuth = (username: string, password: string, totp: string) => {
    setErr('')
    send({
      type: 'auth',
      sender: username,
      body: password,
      totp_code: totp || undefined,
      device_info: `web/${window.location.hostname}`,
    })
  }

  const sendFriendRequest = (to: string) => {
    send({ type: 'friend_request', sender: me ?? undefined, recipient: to })
  }

  const acceptFriend = (from: string) => {
    send({ type: 'friend_accept', sender: from })
    setPending((p) => p.filter((x) => x !== from))
  }

  const openChat = (name: string) => {
    setSelected(name)
    setLog([])
    setE2eReady(!!peerKeysRef.current[name])
    if (!peerKeysRef.current[name]) {
      send({ type: 'key_request', sender: me ?? undefined, recipient: name })
    }
  }

  const sendChat = () => {
    if (!selected || !me || !draft.trim()) return
    const peer = peerKeysRef.current[selected]
    let body = draft.trim()
    if (peer) {
      body = encryptForPeer(body, peer, keysRef.current.secretKey)
    }
    send({ type: 'msg', sender: me, recipient: selected, body })
    appendLog(me, draft.trim(), true)
    playPhazeSound('MessageOutgoing.wav')
    setDraft('')
  }

  const doRegister = () => {
    setErr('')
    if (regUser.length < 3 || regUser.length > 32) {
      setErr('Username must be 3–32 characters')
      return
    }
    if (regPass.length < 8) {
      setErr('Password must be at least 8 characters')
      return
    }
    if (!regEmail.includes('@')) {
      setErr('Enter a valid email')
      return
    }
    send({
      type: 'register',
      sender: regUser,
      body: regPass,
      email: regEmail,
    })
  }

  const doVerify = () => {
    setErr('')
    if (!/^\d{6}$/.test(regCode.trim())) {
      setErr('Enter the 6-digit code from your email')
      return
    }
    send({
      type: 'verify_email',
      sender: regUser,
      body: regCode.trim(),
    })
  }

  // Delete-account flow: state declared above with the rest of the auth
  // state so the WS handler can call setDeleteOpen et al. without TDZ.
  const requestAccountDelete = () => {
    if (!me) return
    if (deleteConfirmText !== 'delete my account') {
      setErr('Type "delete my account" exactly to confirm.')
      return
    }
    if (!deletePassword) {
      setErr('Password required to delete account.')
      return
    }
    setErr('')
    send({ type: 'delete_account', sender: me, body: deletePassword })
    setDeletePassword('')
    setDeleteConfirmText('')
    setDeleteOpen(false)
  }

  return (
    <div className="app">
      <header className="top">
        <div className="brand">
          <h1>Phaze</h1>
          <p className="tagline">Messaging &amp; calls — sovereign, not corporate.</p>
        </div>
        {me && (
          <div className="view-switch" role="tablist" aria-label="View">
            <button
              type="button"
              role="tab"
              aria-selected={view === 'dms'}
              className={view === 'dms' ? 'on' : ''}
              onClick={() => setView('dms')}
            >
              Chats
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={view === 'spaces'}
              className={view === 'spaces' ? 'on' : ''}
              onClick={() => setView('spaces')}
            >
              Spaces
            </button>
          </div>
        )}
        <span className={`pill ${conn === 'open' ? 'ok' : ''}`}>{conn}</span>
        {me && <span className="me">@{me}</span>}
      </header>

      {err && <div className="banner">{err}</div>}

      {me && view === 'spaces' ? (
        <Spaces me={me} send={send} subscribe={subscribe} />
      ) : (

      <main className="grid">
        <section className="panel">
          <h2>Connect</h2>
          <p className="muted small">{wsUrl}</p>
          {!me ? (
            mode === 'login' ? (
              <div className="form">
                <input
                  placeholder="Username"
                  value={loginUser}
                  onChange={(e) => setLoginUser(e.target.value)}
                  autoComplete="username"
                />
                <input
                  type="password"
                  placeholder="Password"
                  value={loginPass}
                  onChange={(e) => setLoginPass(e.target.value)}
                  autoComplete="current-password"
                />
                <input
                  placeholder="TOTP (if enabled)"
                  value={loginTotp}
                  onChange={(e) => setLoginTotp(e.target.value)}
                />
                <button type="button" onClick={() => doAuth(loginUser.trim(), loginPass, loginTotp.trim())}>
                  Sign in
                </button>
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => {
                    setMode('register')
                    setErr('')
                    setRegStep('form')
                  }}
                >
                  Create an account
                </button>
              </div>
            ) : regStep === 'form' ? (
              <div className="form">
                <input
                  placeholder="Choose a username (3–32 chars)"
                  value={regUser}
                  onChange={(e) => setRegUser(e.target.value)}
                  autoComplete="username"
                />
                <input
                  type="email"
                  placeholder="Email"
                  value={regEmail}
                  onChange={(e) => setRegEmail(e.target.value)}
                  autoComplete="email"
                />
                <input
                  type="password"
                  placeholder="Password (8+ chars)"
                  value={regPass}
                  onChange={(e) => setRegPass(e.target.value)}
                  autoComplete="new-password"
                />
                <button type="button" onClick={doRegister}>
                  Create account
                </button>
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => {
                    setMode('login')
                    setErr('')
                  }}
                >
                  Back to sign in
                </button>
              </div>
            ) : (
              <div className="form">
                <p className="muted small">
                  We sent a 6-digit verification code to <strong>{regEmail}</strong>. Paste it below.
                </p>
                <input
                  inputMode="numeric"
                  pattern="\d{6}"
                  maxLength={6}
                  placeholder="123456"
                  value={regCode}
                  onChange={(e) => setRegCode(e.target.value)}
                />
                <button type="button" onClick={doVerify}>
                  Verify email
                </button>
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => {
                    setMode('login')
                    setErr('')
                    setRegStep('form')
                  }}
                >
                  Cancel
                </button>
              </div>
            )
          ) : (
            <>
              <div className="form">
                <input
                  placeholder="Friend username"
                  value={addFriend}
                  onChange={(e) => setAddFriend(e.target.value)}
                />
                <button
                  type="button"
                  onClick={() => {
                    sendFriendRequest(addFriend.trim())
                    setAddFriend('')
                  }}
                >
                  Add friend
                </button>
              </div>
              {turn && <p className="muted small">TURN: {turn.url}</p>}
            </>
          )}
        </section>

        {me && (
          <>
            <section className="panel">
              <h2>Friends</h2>
              <ul className="list">
                {Object.entries(friends).map(([u, st]) => (
                  <li key={u}>
                    <button type="button" className={selected === u ? 'sel' : ''} onClick={() => openChat(u)}>
                      {u} <span className="muted">{st}</span>
                    </button>
                  </li>
                ))}
              </ul>
              {pending.length > 0 && (
                <>
                  <h3>Requests</h3>
                  {pending.map((u) => (
                    <div key={u} className="row">
                      <span>{u}</span>
                      <button type="button" onClick={() => acceptFriend(u)}>
                        Accept
                      </button>
                    </div>
                  ))}
                </>
              )}
            </section>

            <section className="panel grow">
              <h2>{selected ? `Chat — ${selected}` : 'Select a friend'}</h2>
              <div className="chat">
                {log.map((line) => (
                  <div key={line.id} className={`bubble ${line.me ? 'me' : ''}`}>
                    <span className="who">{line.from}</span>
                    {line.text}
                  </div>
                ))}
              </div>
              {selected && (
                <div className="row send">
                  <input
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && sendChat()}
                    placeholder={e2eReady ? 'Message (E2EE)' : 'Message (plaintext until keys exchanged)'}
                  />
                  <button type="button" onClick={sendChat}>
                    Send
                  </button>
                </div>
              )}
            </section>

            <section className="panel danger">
              <h2>Account</h2>
              {!deleteOpen ? (
                <button type="button" className="danger-btn" onClick={() => setDeleteOpen(true)}>
                  Delete account…
                </button>
              ) : (
                <div className="form">
                  <p className="muted small">
                    This erases your account, friends, messages, sessions, and
                    encryption keys on the server. <strong>This cannot be undone.</strong>
                  </p>
                  <input
                    placeholder='Type "delete my account" to confirm'
                    value={deleteConfirmText}
                    onChange={(e) => setDeleteConfirmText(e.target.value)}
                  />
                  <input
                    type="password"
                    placeholder="Your password"
                    value={deletePassword}
                    onChange={(e) => setDeletePassword(e.target.value)}
                    autoComplete="current-password"
                  />
                  <div className="row">
                    <button
                      type="button"
                      className="danger-btn"
                      onClick={requestAccountDelete}
                      disabled={deleteConfirmText !== 'delete my account' || !deletePassword}
                    >
                      Erase my account
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setDeleteOpen(false)
                        setDeletePassword('')
                        setDeleteConfirmText('')
                      }}
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              )}
            </section>
          </>
        )}
      </main>
      )}

      <footer className="foot muted small">
        Beta — session and NaCl keys are stored in localStorage. Use HTTPS in production; set Phaze_ALLOWED_ORIGINS for your web origin.
      </footer>
    </div>
  )
}
