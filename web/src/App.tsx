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
import Settings from './Settings'
import './App.css'

const SESSION_KEY = 'phaze_session_token_v1'
const KEYS_KEY = 'phaze_nacl_keys_v1'

type ChatLine = { id: string; from: string; text: string; me: boolean }
type CallState = {
  peer: string
  type: 'audio' | 'video'
  status: 'ringing' | 'active'
  direction: 'outgoing' | 'incoming'
}

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

function statusColor(st: string): string {
  if (st === 'Online') return '#22c55e'
  if (st === 'Away' || st === 'away') return '#f59e0b'
  if (st === 'Do Not Disturb' || st === 'dnd') return '#ef4444'
  return '#94a3b8'
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
  const [typingPeers, setTypingPeers] = useState<Set<string>>(new Set())
  const [callState, setCallState] = useState<CallState | null>(null)

  // Auth + registration UI state hoisted above WS handler (ESLint no-use-before-define)
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

  const [view, setView] = useState<'dms' | 'spaces'>('dms')
  const [settingsOpen, setSettingsOpen] = useState(false)

  const subscribersRef = useRef(new Set<(m: NexusMessage) => void>())
  const subscribe = useCallback((handler: (m: NexusMessage) => void) => {
    subscribersRef.current.add(handler)
    return () => { subscribersRef.current.delete(handler) }
  }, [])

  const wsRef = useRef<WebSocket | null>(null)
  const keysRef = useRef(loadOrCreateKeys())
  const peerKeysRef = useRef<Record<string, Uint8Array>>({})
  const pinsRef = useRef(loadPins())
  const meRef = useRef<string | null>(null)
  const selectedRef = useRef<string | null>(null)
  const sendRef = useRef<(m: NexusMessage) => void>(() => {})
  const callStateRef = useRef<CallState | null>(null)
  const turnRef = useRef<TurnConfig | null>(null)
  const typingTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const outTypingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // WebRTC refs
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const localStreamRef = useRef<MediaStream | null>(null)
  const incomingCallSdpRef = useRef<string | null>(null)
  const localVideoRef = useRef<HTMLVideoElement>(null)
  const remoteVideoRef = useRef<HTMLVideoElement>(null)

  useEffect(() => { meRef.current = me }, [me])
  useEffect(() => { selectedRef.current = selected }, [selected])
  useEffect(() => { callStateRef.current = callState }, [callState])
  useEffect(() => { turnRef.current = turn }, [turn])

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
      if (peer === selectedRef.current) setE2eReady(true)
    })()
  }, [])

  const unwrap = useCallback((msg: NexusMessage): NexusMessage => {
    const sender = msg.sender ?? ''
    if (!sender) return msg
    const pk = peerKeysRef.current[sender]
    const sk = keysRef.current.secretKey
    const out = { ...msg }
    // Only decrypt body for chat messages — call signaling is not encrypted
    if (out.body && pk && msg.type === 'msg') out.body = decryptFromPeer(out.body, pk, sk)
    return out
  }, [])

  const tearDownCall = useCallback(() => {
    pcRef.current?.close()
    pcRef.current = null
    localStreamRef.current?.getTracks().forEach((t) => t.stop())
    localStreamRef.current = null
    incomingCallSdpRef.current = null
    setCallState(null)
  }, [])

  const hangUp = useCallback(() => {
    const cs = callStateRef.current
    if (cs) {
      const type = cs.status === 'ringing' ? 'call_reject' : 'call_end'
      sendRef.current({ type, recipient: cs.peer })
    }
    tearDownCall()
  }, [tearDownCall])

  const makePC = useCallback((recipient: string): RTCPeerConnection => {
    const iceServers: RTCIceServer[] = turnRef.current
      ? [{ urls: turnRef.current.url, username: turnRef.current.username, credential: turnRef.current.password }]
      : [{ urls: 'stun:stun.l.google.com:19302' }]
    const pc = new RTCPeerConnection({ iceServers })
    pc.ontrack = (e) => {
      if (remoteVideoRef.current && e.streams[0]) remoteVideoRef.current.srcObject = e.streams[0]
    }
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        sendRef.current({ type: 'ice_candidate', recipient, candidate: JSON.stringify(e.candidate) })
      }
    }
    return pc
  }, [])

  const startCall = useCallback(async (type: 'audio' | 'video') => {
    const recipient = selectedRef.current
    if (!recipient || !meRef.current) return
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: type === 'video' })
    } catch {
      setErr('Microphone/camera access denied. Check browser permissions.')
      return
    }
    const pc = makePC(recipient)
    pcRef.current = pc
    localStreamRef.current = stream
    stream.getTracks().forEach((t) => pc.addTrack(t, stream))
    if (localVideoRef.current && type === 'video') localVideoRef.current.srcObject = stream
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    sendRef.current({ type: 'call_offer', recipient, sdp: offer.sdp, body: type })
    setCallState({ peer: recipient, type, status: 'ringing', direction: 'outgoing' })
  }, [makePC])

  const acceptCall = useCallback(async () => {
    const cs = callStateRef.current
    const sdp = incomingCallSdpRef.current
    if (!cs || !sdp) return
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: cs.type === 'video' })
    } catch {
      setErr('Microphone/camera access denied. Check browser permissions.')
      hangUp()
      return
    }
    const pc = makePC(cs.peer)
    pcRef.current = pc
    localStreamRef.current = stream
    stream.getTracks().forEach((t) => pc.addTrack(t, stream))
    if (localVideoRef.current && cs.type === 'video') localVideoRef.current.srcObject = stream
    await pc.setRemoteDescription({ type: 'offer', sdp })
    const answer = await pc.createAnswer()
    await pc.setLocalDescription(answer)
    sendRef.current({ type: 'call_answer', recipient: cs.peer, sdp: answer.sdp })
    incomingCallSdpRef.current = null
    setCallState({ ...cs, status: 'active' })
  }, [makePC, hangUp])

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
          if (msg.sender && pk && pk.length === 32) acceptPeerKey(msg.sender, pk, msg.key_fingerprint || '')
          if (msg.sender && msg.status) setFriends((f) => ({ ...f, [msg.sender!]: msg.status! }))
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
            if (msg.sender !== meRef.current) playPhazeSound('MessageReceived.wav')
          }
          break

        case 'typing':
          if (msg.sender && msg.sender !== meRef.current) {
            const peer = msg.sender
            setTypingPeers((p) => new Set([...p, peer]))
            clearTimeout(typingTimersRef.current[peer])
            typingTimersRef.current[peer] = setTimeout(() => {
              setTypingPeers((p) => { const n = new Set(p); n.delete(peer); return n })
            }, 3000)
          }
          break

        case 'call_offer':
          if (msg.sender && msg.sdp) {
            incomingCallSdpRef.current = msg.sdp
            setCallState({ peer: msg.sender, type: (msg.body as 'audio' | 'video') || 'audio', status: 'ringing', direction: 'incoming' })
          }
          break

        case 'call_answer':
          if (msg.sdp && pcRef.current) {
            void pcRef.current.setRemoteDescription({ type: 'answer', sdp: msg.sdp })
            setCallState((prev) => prev ? { ...prev, status: 'active' } : null)
          }
          break

        case 'ice_candidate':
          if (msg.candidate && pcRef.current) {
            try { void pcRef.current.addIceCandidate(JSON.parse(msg.candidate)) } catch { /* stale candidate */ }
          }
          break

        case 'call_reject':
        case 'call_end':
          tearDownCall()
          break

        case 'kicked':
          localStorage.removeItem(SESSION_KEY)
          setErr(msg.body || 'Kicked')
          setMe(null)
          break

        case 'delete_account_result':
          if (msg.status === 'ok') {
            localStorage.removeItem(SESSION_KEY)
            localStorage.removeItem(KEYS_KEY)
            peerKeysRef.current = {}
            pinsRef.current = {}
            try { localStorage.removeItem('phaze_key_pins_v1') } catch { /* fine */ }
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

      subscribersRef.current.forEach((sub) => {
        try { sub(msg) } catch { /* swallow */ }
      })
    }
  }, [unwrap, appendLog, acceptPeerKey, tearDownCall])

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
        sendRef.current({ type: 'session_auth', qr_token: tok, device_info: `web/${host}` })
      }
    }

    w.onmessage = (e: MessageEvent) => {
      try {
        onMessageRef.current(JSON.parse(e.data as string) as NexusMessage)
      } catch { /* malformed */ }
    }

    w.onclose = () => {
      setConn('off')
      wsRef.current = null
    }

    w.onerror = () => setErr('WebSocket error')

    return () => {
      w.close()
      wsRef.current = null
    }
  }, [wsUrl])

  const send = useCallback((m: NexusMessage) => { sendRef.current(m) }, [])

  const doAuth = (username: string, password: string, totp: string) => {
    setErr('')
    send({ type: 'auth', sender: username, body: password, totp_code: totp || undefined, device_info: `web/${window.location.hostname}` })
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
    if (peer) body = encryptForPeer(body, peer, keysRef.current.secretKey)
    send({ type: 'msg', sender: me, recipient: selected, body })
    appendLog(me, draft.trim(), true)
    playPhazeSound('MessageOutgoing.wav')
    setDraft('')
  }

  const handleDraftChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setDraft(e.target.value)
    if (selectedRef.current && meRef.current) {
      if (!outTypingTimerRef.current) {
        sendRef.current({ type: 'typing', recipient: selectedRef.current })
      }
      clearTimeout(outTypingTimerRef.current ?? undefined)
      outTypingTimerRef.current = setTimeout(() => { outTypingTimerRef.current = null }, 2000)
    }
  }

  const doRegister = () => {
    setErr('')
    if (regUser.length < 3 || regUser.length > 32) { setErr('Username must be 3–32 characters'); return }
    if (regPass.length < 8) { setErr('Password must be at least 8 characters'); return }
    if (!regEmail.includes('@')) { setErr('Enter a valid email'); return }
    send({ type: 'register', sender: regUser, body: regPass, email: regEmail })
  }

  const doVerify = () => {
    setErr('')
    if (!/^\d{6}$/.test(regCode.trim())) { setErr('Enter the 6-digit code from your email'); return }
    send({ type: 'verify_email', sender: regUser, body: regCode.trim() })
  }

  const requestAccountDelete = () => {
    if (!me) return
    if (deleteConfirmText !== 'delete my account') { setErr('Type "delete my account" exactly to confirm.'); return }
    if (!deletePassword) { setErr('Password required to delete account.'); return }
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
            <button type="button" role="tab" aria-selected={view === 'dms'} className={view === 'dms' ? 'on' : ''} onClick={() => setView('dms')}>Chats</button>
            <button type="button" role="tab" aria-selected={view === 'spaces'} className={view === 'spaces' ? 'on' : ''} onClick={() => setView('spaces')}>Spaces</button>
          </div>
        )}
        <span className={`pill ${conn === 'open' ? 'ok' : ''}`}>{conn}</span>
        {me && (
          <button className="settings-gear" title="Settings" onClick={() => setSettingsOpen(true)}>⚙</button>
        )}
        {me && <span className="me">@{me}</span>}
      </header>

      {err && <div className="banner">{err}</div>}

      {settingsOpen && me && (
        <Settings me={me} send={send} subscribe={subscribe} onClose={() => setSettingsOpen(false)} />
      )}

      {/* ── Call overlay ─────────────────────────────────────────── */}
      {callState && (
        <div className="call-overlay">
          {callState.type === 'video' && (
            <div className="call-videos">
              <video ref={remoteVideoRef} autoPlay playsInline className="call-remote" />
              <video ref={localVideoRef} autoPlay playsInline muted className="call-local" />
            </div>
          )}
          <div className="call-card">
            <div className="call-avatar">{callState.peer[0].toUpperCase()}</div>
            <div className="call-peer-name">{callState.peer}</div>
            <div className="call-status-text">
              {callState.status === 'ringing' && callState.direction === 'outgoing' && 'Calling…'}
              {callState.status === 'ringing' && callState.direction === 'incoming' && `${callState.type === 'video' ? '📹' : '☎'} Incoming ${callState.type} call`}
              {callState.status === 'active' && 'Connected'}
            </div>
            <div className="call-controls">
              {callState.direction === 'incoming' && callState.status === 'ringing' ? (
                <>
                  <button className="call-btn-accept" onClick={() => void acceptCall()}>Accept</button>
                  <button className="call-btn-decline" onClick={hangUp}>Decline</button>
                </>
              ) : (
                <button className="call-btn-end" onClick={hangUp}>End call</button>
              )}
            </div>
          </div>
        </div>
      )}

      {me && view === 'spaces' ? (
        <Spaces me={me} send={send} subscribe={subscribe} />
      ) : (
        <main className="grid">
          {/* ── Panel 1: connect / auth ───────────────────────────── */}
          <section className="panel">
            <h2>Connect</h2>
            {!me ? (
              mode === 'login' ? (
                <div className="form">
                  <input placeholder="Username" value={loginUser} onChange={(e) => setLoginUser(e.target.value)} autoComplete="username" />
                  <input type="password" placeholder="Password" value={loginPass} onChange={(e) => setLoginPass(e.target.value)} autoComplete="current-password" />
                  <input placeholder="TOTP (if enabled)" value={loginTotp} onChange={(e) => setLoginTotp(e.target.value)} />
                  <button type="button" onClick={() => doAuth(loginUser.trim(), loginPass, loginTotp.trim())}>Sign in</button>
                  <button type="button" className="link-btn" onClick={() => { setMode('register'); setErr(''); setRegStep('form') }}>Create an account</button>
                </div>
              ) : regStep === 'form' ? (
                <div className="form">
                  <input placeholder="Choose a username (3–32 chars)" value={regUser} onChange={(e) => setRegUser(e.target.value)} autoComplete="username" />
                  <input type="email" placeholder="Email" value={regEmail} onChange={(e) => setRegEmail(e.target.value)} autoComplete="email" />
                  <input type="password" placeholder="Password (8+ chars)" value={regPass} onChange={(e) => setRegPass(e.target.value)} autoComplete="new-password" />
                  <button type="button" onClick={doRegister}>Create account</button>
                  <button type="button" className="link-btn" onClick={() => { setMode('login'); setErr('') }}>Back to sign in</button>
                </div>
              ) : (
                <div className="form">
                  <p className="muted small">We sent a 6-digit code to <strong>{regEmail}</strong>.</p>
                  <input inputMode="numeric" pattern="\d{6}" maxLength={6} placeholder="123456" value={regCode} onChange={(e) => setRegCode(e.target.value)} />
                  <button type="button" onClick={doVerify}>Verify email</button>
                  <button type="button" className="link-btn" onClick={() => { setMode('login'); setErr(''); setRegStep('form') }}>Cancel</button>
                </div>
              )
            ) : (
              <>
                <div className="form">
                  <input placeholder="Friend username" value={addFriend} onChange={(e) => setAddFriend(e.target.value)} />
                  <button type="button" onClick={() => { sendFriendRequest(addFriend.trim()); setAddFriend('') }}>Add friend</button>
                </div>
                {turn && <p className="muted small">TURN: {turn.url}</p>}
              </>
            )}
          </section>

          {/* ── Panel 2: friends list ─────────────────────────────── */}
          {me && (
            <>
              <section className="panel">
                <h2>Friends</h2>
                <ul className="list">
                  {Object.entries(friends).map(([u, st]) => (
                    <li key={u}>
                      <button type="button" className={selected === u ? 'sel' : ''} onClick={() => openChat(u)}>
                        <span className="status-dot" style={{ background: statusColor(st) }} />
                        <span className="friend-name">{u}</span>
                        <span className="friend-st muted">{st}</span>
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
                        <button type="button" onClick={() => acceptFriend(u)}>Accept</button>
                      </div>
                    ))}
                  </>
                )}
              </section>

              {/* ── Panel 3: chat ─────────────────────────────────── */}
              <section className="panel grow">
                <div className="chat-header-bar">
                  {selected ? (
                    <>
                      <span className="status-dot" style={{ background: statusColor(friends[selected] ?? 'Offline') }} />
                      <span className="chat-peer-name">{selected}</span>
                      <span className="chat-peer-status muted small">{friends[selected] ?? 'Offline'}</span>
                      <div className="chat-call-btns">
                        <button
                          type="button"
                          className="chat-call-btn"
                          title="Audio call"
                          onClick={() => void startCall('audio')}
                          disabled={!me}
                        >☎</button>
                        <button
                          type="button"
                          className="chat-call-btn"
                          title="Video call"
                          onClick={() => void startCall('video')}
                          disabled={!me}
                        >📹</button>
                      </div>
                    </>
                  ) : (
                    <span className="muted small">Select a friend to chat</span>
                  )}
                </div>

                <div className="chat">
                  {log.map((line) => (
                    <div key={line.id} className={`bubble ${line.me ? 'me' : ''}`}>
                      <span className="who">{line.from}</span>
                      {line.text}
                    </div>
                  ))}
                  {selected && typingPeers.has(selected) && (
                    <div className="typing-indicator">
                      <span>{selected} is typing</span>
                      <span className="typing-dots"><span /><span /><span /></span>
                    </div>
                  )}
                </div>

                {selected && (
                  <div className="row send">
                    <input
                      value={draft}
                      onChange={handleDraftChange}
                      onKeyDown={(e) => e.key === 'Enter' && sendChat()}
                      placeholder={e2eReady ? 'Message (E2EE)' : 'Message'}
                    />
                    <button type="button" onClick={sendChat}>Send</button>
                  </div>
                )}
              </section>

              {/* Panel 4 removed — account management is in ⚙ Settings */}
            </>
          )}
        </main>
      )}

      <footer className="foot muted small">
        Beta — session and NaCl keys are stored in localStorage. Use HTTPS in production.
      </footer>
    </div>
  )
}
