import { useEffect, useMemo, useRef, useState } from 'react'
import type { ChannelInfo, ChannelMsg, NexusMessage, ServerSummary } from './nexusTypes'
import './spaces.css'

interface Props {
  me: string
  /** sends a NexusMessage on the parent socket. */
  send: (m: NexusMessage) => void
  /** lets us subscribe to every inbound message (for server_ / channel_ types). */
  subscribe: (handler: (m: NexusMessage) => void) => () => void
}

interface Toast {
  id: number
  body: string
  tone: 'info' | 'error' | 'ok'
}

let toastSeq = 0

export default function Spaces({ me, send, subscribe }: Props) {
  const [servers, setServers] = useState<ServerSummary[]>([])
  const [activeServer, setActiveServer] = useState<string | null>(null)
  const [channelsByServer, setChannelsByServer] = useState<Record<string, ChannelInfo[]>>({})
  const [activeChannel, setActiveChannel] = useState<string | null>(null)
  const [messagesByChannel, setMessagesByChannel] = useState<Record<string, ChannelMsg[]>>({})
  const [, setMemberCountByServer] = useState<Record<string, number>>({})
  const [draft, setDraft] = useState('')
  const [composerOpen, setComposerOpen] = useState<null | 'create' | 'join'>(null)
  const [newName, setNewName] = useState('')
  const [newTopic, setNewTopic] = useState('')
  const [newVisibility, setNewVisibility] = useState<'public' | 'private'>('private')
  const [joinCode, setJoinCode] = useState('')
  const [newChannelOpen, setNewChannelOpen] = useState(false)
  const [newChannelName, setNewChannelName] = useState('')
  const [toasts, setToasts] = useState<Toast[]>([])
  const chatBottomRef = useRef<HTMLDivElement | null>(null)

  const toast = (body: string, tone: Toast['tone'] = 'info') => {
    const id = ++toastSeq
    setToasts((t) => [...t, { id, body, tone }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4500)
  }

  const activeChannelInfo = useMemo(() => {
    if (!activeServer || !activeChannel) return null
    const chans = channelsByServer[activeServer] ?? []
    return chans.find((c) => c.id === activeChannel) ?? null
  }, [activeServer, activeChannel, channelsByServer])

  const activeServerInfo = useMemo(
    () => servers.find((s) => s.id === activeServer) ?? null,
    [servers, activeServer],
  )

  // Initial server list + subscribe to push updates.
  useEffect(() => {
    send({ type: 'server_list' })
    const unsub = subscribe((m) => {
      switch (m.type) {
        case 'server_list_result':
          if (m.status === 'ok') setServers(m.servers ?? [])
          break
        case 'server_result':
          if (m.status === 'ok' && m.server_id) {
            const summary: ServerSummary = {
              id: m.server_id,
              name: m.server_name ?? 'Untitled',
              owner: me,
              role: 'owner',
              visibility: (m.visibility as 'public' | 'private') ?? 'private',
              invite_code: m.invite_code,
            }
            setServers((prev) => [...prev.filter((s) => s.id !== summary.id), summary])
            if (m.channels) setChannelsByServer((c) => ({ ...c, [summary.id]: m.channels! }))
            setActiveServer(summary.id)
            if (m.channels?.length) setActiveChannel(m.channels[0].id)
            toast(`Space created — share code ${m.invite_code}`, 'ok')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'server_join_result':
          if (m.status === 'ok' && m.server_id) {
            send({ type: 'server_list' })
            if (m.channels) setChannelsByServer((c) => ({ ...c, [m.server_id!]: m.channels! }))
            setActiveServer(m.server_id)
            if (m.channels?.length) setActiveChannel(m.channels[0].id)
            toast(`Joined ${m.server_name ?? 'space'}`, 'ok')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'server_leave_result':
          if (m.status === 'ok' && m.server_id) {
            setServers((prev) => prev.filter((s) => s.id !== m.server_id))
            if (activeServer === m.server_id) {
              setActiveServer(null)
              setActiveChannel(null)
            }
            toast('Left space', 'info')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'server_info_result':
          if (m.status === 'ok' && m.server_id) {
            if (m.channels) setChannelsByServer((c) => ({ ...c, [m.server_id!]: m.channels! }))
            if (m.members) setMemberCountByServer((mc) => ({ ...mc, [m.server_id!]: m.members!.length }))
          }
          break
        case 'server_channels_updated':
          if (m.server_id && m.channels) {
            setChannelsByServer((c) => ({ ...c, [m.server_id!]: m.channels! }))
          }
          break
        case 'channel_result':
          if (m.status === 'ok' && m.channel_id) {
            setActiveChannel(m.channel_id)
            setNewChannelOpen(false)
            setNewChannelName('')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'channel_history_result':
          if (m.status === 'ok' && m.channel_id && m.messages) {
            setMessagesByChannel((mc) => ({ ...mc, [m.channel_id!]: m.messages! }))
          }
          break
        case 'channel_msg_in':
          if (m.channel_id && m.messages && m.messages[0]) {
            const incoming = m.messages[0]
            setMessagesByChannel((mc) => {
              const existing = mc[m.channel_id!] ?? []
              if (existing.some((x) => x.id === incoming.id)) return mc
              return { ...mc, [m.channel_id!]: [...existing, incoming] }
            })
          }
          break
        case 'channel_msg_result':
          if (m.error) toast(m.error, 'error')
          break
      }
    })
    return unsub
  }, [me, send, subscribe, activeServer])

  // Load channels + history when active server/channel changes.
  useEffect(() => {
    if (!activeServer) return
    if (!channelsByServer[activeServer]) {
      send({ type: 'server_info', server_id: activeServer })
    }
  }, [activeServer, channelsByServer, send])

  useEffect(() => {
    if (!activeChannel) return
    if (!messagesByChannel[activeChannel]) {
      send({ type: 'channel_history', channel_id: activeChannel })
    }
  }, [activeChannel, messagesByChannel, send])

  // Auto-scroll on new messages.
  useEffect(() => {
    if (!chatBottomRef.current) return
    chatBottomRef.current.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [activeChannel, messagesByChannel])

  const submitMessage = () => {
    if (!activeChannel) return
    const body = draft.trim()
    if (!body) return
    send({ type: 'channel_msg', channel_id: activeChannel, body })
    setDraft('')
  }

  const createServer = () => {
    if (!newName.trim()) {
      toast('Pick a name', 'error')
      return
    }
    send({
      type: 'server_create',
      server_name: newName.trim(),
      topic: newTopic.trim(),
      visibility: newVisibility,
    })
    setComposerOpen(null)
    setNewName('')
    setNewTopic('')
  }

  const joinServer = () => {
    if (!joinCode.trim()) {
      toast('Paste an invite code', 'error')
      return
    }
    send({ type: 'server_join', invite_code: joinCode.trim() })
    setComposerOpen(null)
    setJoinCode('')
  }

  const createChannel = () => {
    if (!activeServer || !newChannelName.trim()) return
    send({
      type: 'channel_create',
      server_id: activeServer,
      channel_name: newChannelName.trim().toLowerCase().replace(/\s+/g, '-'),
      kind: 'text',
    })
  }

  const leaveServer = () => {
    if (!activeServer) return
    if (!confirm('Leave this space?')) return
    send({ type: 'server_leave', server_id: activeServer })
  }

  const copyInvite = async () => {
    if (!activeServerInfo?.invite_code) return
    try {
      await navigator.clipboard.writeText(activeServerInfo.invite_code)
      toast('Invite code copied', 'ok')
    } catch {
      toast(`Invite: ${activeServerInfo.invite_code}`, 'info')
    }
  }

  return (
    <div className="spaces-root">
      <aside className="server-rail">
        <button
          className="server-icon home"
          aria-label="Direct messages — back to chat"
          title="(future: jump back to DMs)"
        >
          <SkypeMark />
        </button>
        <div className="rail-divider" />
        {servers.map((s) => (
          <button
            key={s.id}
            type="button"
            className={`server-icon ${activeServer === s.id ? 'active' : ''}`}
            onClick={() => {
              setActiveServer(s.id)
              const chans = channelsByServer[s.id] ?? []
              if (chans.length) setActiveChannel(chans[0].id)
            }}
            title={s.name}
          >
            <span className="server-icon-letter">{initials(s.name)}</span>
            <span className="server-tooltip">{s.name}</span>
          </button>
        ))}
        <button
          className="server-icon add"
          type="button"
          onClick={() => setComposerOpen('create')}
          title="Create a new space"
          aria-label="Create space"
        >
          +
        </button>
        <button
          className="server-icon add ghost"
          type="button"
          onClick={() => setComposerOpen('join')}
          title="Join with invite code"
          aria-label="Join space"
        >
          ⤵
        </button>
      </aside>

      <section className="channel-pane">
        {activeServerInfo ? (
          <>
            <header className="channel-pane-head">
              <div className="server-name-block">
                <h2>{activeServerInfo.name}</h2>
                <span className={`vis-pill ${activeServerInfo.visibility}`}>
                  {activeServerInfo.visibility}
                </span>
              </div>
              {activeServerInfo.invite_code && (
                <button type="button" className="ghost-btn" onClick={copyInvite}>
                  Invite · {activeServerInfo.invite_code}
                </button>
              )}
            </header>
            <div className="channel-list">
              {(channelsByServer[activeServer!] ?? []).map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className={`channel-row ${activeChannel === c.id ? 'active' : ''} kind-${c.kind}`}
                  onClick={() => setActiveChannel(c.id)}
                >
                  <span className="channel-hash">{c.kind === 'voice' ? '🎙' : '#'}</span>
                  <span className="channel-name">{c.name}</span>
                </button>
              ))}
              {(activeServerInfo.role === 'owner' || activeServerInfo.role === 'admin') && (
                <>
                  {newChannelOpen ? (
                    <div className="new-channel-row">
                      <input
                        autoFocus
                        placeholder="channel-name"
                        value={newChannelName}
                        onChange={(e) => setNewChannelName(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') createChannel()
                          if (e.key === 'Escape') {
                            setNewChannelOpen(false)
                            setNewChannelName('')
                          }
                        }}
                      />
                      <button type="button" className="mini-btn" onClick={createChannel}>
                        Add
                      </button>
                    </div>
                  ) : (
                    <button
                      type="button"
                      className="channel-row add"
                      onClick={() => setNewChannelOpen(true)}
                    >
                      <span className="channel-hash">+</span>
                      <span className="channel-name">Add channel</span>
                    </button>
                  )}
                </>
              )}
            </div>
            <footer className="channel-pane-foot">
              {activeServerInfo.role !== 'owner' && (
                <button type="button" className="leave-btn" onClick={leaveServer}>
                  Leave space
                </button>
              )}
            </footer>
          </>
        ) : (
          <div className="empty-pane">
            <p className="empty-title">No space selected</p>
            <p className="empty-hint">
              Create your first <strong>Space</strong> or paste an invite code to join one.
            </p>
            <div className="empty-cta">
              <button type="button" onClick={() => setComposerOpen('create')}>
                + Create a space
              </button>
              <button type="button" className="ghost-btn" onClick={() => setComposerOpen('join')}>
                Join with code
              </button>
            </div>
          </div>
        )}
      </section>

      <section className="chat-pane">
        {activeChannelInfo ? (
          <>
            <header className="chat-head">
              <h2>
                <span className="hash">{activeChannelInfo.kind === 'voice' ? '🎙' : '#'}</span>
                {activeChannelInfo.name}
              </h2>
              {activeChannelInfo.topic && <p className="chat-topic">{activeChannelInfo.topic}</p>}
            </header>
            <div className="chat-stream">
              {(messagesByChannel[activeChannel!] ?? []).map((m, idx, arr) => {
                const prev = idx > 0 ? arr[idx - 1] : null
                const groupHead = !prev || prev.sender !== m.sender || gapMins(prev.created_at, m.created_at) > 5
                return (
                  <div
                    key={m.id}
                    className={`chat-msg ${m.sender === me ? 'me' : ''} ${groupHead ? 'head' : 'cont'}`}
                  >
                    {groupHead && (
                      <div className="chat-msg-meta">
                        <span className="avatar" data-initial={initials(m.sender)} />
                        <span className="sender">{m.sender}</span>
                        <span className="ts">{formatTs(m.created_at)}</span>
                      </div>
                    )}
                    <div className="chat-msg-body">{m.body}</div>
                  </div>
                )
              })}
              <div ref={chatBottomRef} />
            </div>
            <footer className="chat-composer">
              <textarea
                placeholder={`Message #${activeChannelInfo.name}`}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    submitMessage()
                  }
                }}
                rows={1}
              />
              <button
                type="button"
                onClick={submitMessage}
                disabled={!draft.trim()}
                className="send-btn"
                aria-label="Send"
              >
                ➤
              </button>
            </footer>
          </>
        ) : (
          <div className="empty-pane lg">
            <p className="empty-title">Pick a channel</p>
            <p className="empty-hint">Channels in this space appear in the middle pane.</p>
          </div>
        )}
      </section>

      {composerOpen === 'create' && (
        <div className="modal-scrim" onClick={() => setComposerOpen(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>Create a space</h3>
            <p className="modal-hint">Spaces are persistent group chats with channels.</p>
            <input
              placeholder="Space name (e.g. Bruh Mode)"
              value={newName}
              autoFocus
              onChange={(e) => setNewName(e.target.value)}
            />
            <input
              placeholder="What's it about? (optional)"
              value={newTopic}
              onChange={(e) => setNewTopic(e.target.value)}
            />
            <div className="vis-row">
              {(['private', 'public'] as const).map((v) => (
                <button
                  key={v}
                  type="button"
                  className={`vis-btn ${newVisibility === v ? 'on' : ''}`}
                  onClick={() => setNewVisibility(v)}
                >
                  {v === 'private' ? '🔒 Private' : '🌐 Public'}
                </button>
              ))}
            </div>
            <div className="modal-actions">
              <button type="button" className="ghost-btn" onClick={() => setComposerOpen(null)}>
                Cancel
              </button>
              <button type="button" onClick={createServer}>
                Create
              </button>
            </div>
          </div>
        </div>
      )}

      {composerOpen === 'join' && (
        <div className="modal-scrim" onClick={() => setComposerOpen(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>Join a space</h3>
            <p className="modal-hint">Paste an invite code from a space owner.</p>
            <input
              placeholder="Invite code"
              value={joinCode}
              autoFocus
              onChange={(e) => setJoinCode(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && joinServer()}
            />
            <div className="modal-actions">
              <button type="button" className="ghost-btn" onClick={() => setComposerOpen(null)}>
                Cancel
              </button>
              <button type="button" onClick={joinServer}>
                Join
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="toast-stack" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast t-${t.tone}`}>
            {t.body}
          </div>
        ))}
      </div>
    </div>
  )
}

function initials(name: string): string {
  if (!name) return '?'
  const parts = name.trim().split(/\s+/)
  if (parts.length === 1) return name.slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

function gapMins(a: string, b: string): number {
  const ta = Date.parse(a)
  const tb = Date.parse(b)
  if (Number.isNaN(ta) || Number.isNaN(tb)) return 0
  return Math.abs(tb - ta) / 60000
}

function formatTs(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const yesterday = new Date(now.getTime() - 86400000).toDateString() === d.toDateString()
  const hhmm = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (sameDay) return hhmm
  if (yesterday) return `Yesterday ${hhmm}`
  return `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${hhmm}`
}

function SkypeMark() {
  return (
    <svg viewBox="0 0 32 32" width="22" height="22" aria-hidden="true">
      <defs>
        <radialGradient id="phzCore" cx="50%" cy="40%" r="60%">
          <stop offset="0%" stopColor="#9be8ff" />
          <stop offset="55%" stopColor="#00aff0" />
          <stop offset="100%" stopColor="#005d99" />
        </radialGradient>
      </defs>
      <circle cx="16" cy="16" r="13" fill="url(#phzCore)" />
      <path d="M11 11c2.5-1.5 7-1.5 9 0 2 1.5 1.5 4-1 4.5l-3 .5c-1.5.3-2 1-1 1.7 1 .8 4 .6 5.5-.6"
            stroke="#fff" strokeWidth="2" fill="none" strokeLinecap="round" />
    </svg>
  )
}
