import { useState, useEffect, useCallback } from 'react'
import type { NexusMessage } from './nexusTypes'
import './settings.css'

type Tab = 'profile' | 'security' | 'privacy' | 'danger'

interface Props {
  me: string
  send: (m: NexusMessage) => void
  subscribe: (handler: (m: NexusMessage) => void) => () => void
  onClose: () => void
}

export default function Settings({ me, send, subscribe, onClose }: Props) {
  const [tab, setTab] = useState<Tab>('profile')

  // Profile
  const [displayName, setDisplayName] = useState('')
  const [mood, setMood] = useState('')
  const [status, setStatus] = useState('Online')
  const [profileMsg, setProfileMsg] = useState('')

  // Security
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [newPw2, setNewPw2] = useState('')
  const [pwMsg, setPwMsg] = useState('')
  const [totpUri, setTotpUri] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [totpMsg, setTotpMsg] = useState('')
  const [totpPending, setTotpPending] = useState(false)

  // Privacy
  const [blocks, setBlocks] = useState<string[]>([])
  const [blockMsg, setBlockMsg] = useState('')

  // Danger
  const [delConfirm, setDelConfirm] = useState('')
  const [delPw, setDelPw] = useState('')
  const [delMsg, setDelMsg] = useState('')

  useEffect(() => {
    // Load blocks on first open
    send({ type: 'list_blocks' })
  }, [send])

  const onMsg = useCallback((msg: NexusMessage) => {
    switch (msg.type) {
      case 'update_result':
        setProfileMsg(msg.status === 'ok' ? 'Saved.' : (msg.error || 'Error'))
        break
      case 'change_password_result':
        if (msg.status === 'ok') {
          setPwMsg('Password changed.')
          setOldPw(''); setNewPw(''); setNewPw2('')
        } else {
          setPwMsg(msg.error || 'Error')
        }
        break
      case 'totp_result':
        if (msg.status === 'pending_confirm' && msg.totp_uri) {
          setTotpUri(msg.totp_uri)
          setTotpPending(true)
          setTotpMsg('Scan QR in your authenticator app, then enter the code below.')
        } else if (msg.status === 'enabled') {
          setTotpMsg('2FA enabled.')
          setTotpPending(false)
          setTotpUri('')
          setTotpCode('')
        } else if (msg.status === 'disabled') {
          setTotpMsg('2FA disabled.')
          setTotpPending(false)
        } else {
          setTotpMsg(msg.error || 'Error')
        }
        break
      case 'blocks':
        if (msg.results) setBlocks(msg.results)
        break
      case 'block_result':
        if (msg.status === 'unblocked' && msg.recipient) {
          setBlocks((b) => b.filter((x) => x !== msg.recipient))
          setBlockMsg(`Unblocked ${msg.recipient}.`)
        }
        break
      case 'delete_account_result':
        if (msg.status !== 'ok') setDelMsg(msg.error || 'Error')
        break
    }
  }, [])

  useEffect(() => subscribe(onMsg), [subscribe, onMsg])

  const saveProfile = () => {
    setProfileMsg('')
    send({ type: 'update_profile', sender: me, mood, display_name: displayName })
    if (status) send({ type: 'status_update', body: status })
  }

  const changePassword = () => {
    setPwMsg('')
    if (newPw !== newPw2) { setPwMsg('New passwords do not match'); return }
    if (newPw.length < 8) { setPwMsg('New password must be 8+ characters'); return }
    send({ type: 'change_password', body: `${oldPw}:${newPw}` })
  }

  const enableTotp = () => {
    setTotpMsg('')
    send({ type: 'enable_totp' })
  }

  const confirmTotp = () => {
    setTotpMsg('')
    send({ type: 'confirm_totp', totp_code: totpCode })
  }

  const disableTotp = () => {
    setTotpMsg('')
    if (!oldPw) { setTotpMsg('Enter your current password to disable 2FA'); return }
    send({ type: 'disable_totp', body: oldPw })
  }

  const unblock = (user: string) => {
    setBlockMsg('')
    send({ type: 'unblock', recipient: user })
  }

  const deleteAccount = () => {
    setDelMsg('')
    if (delConfirm !== 'delete my account') { setDelMsg('Type "delete my account" exactly'); return }
    if (!delPw) { setDelMsg('Password required'); return }
    send({ type: 'delete_account', sender: me, body: delPw })
  }

  return (
    <div className="settings-overlay" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div className="settings-modal">
        <div className="settings-header">
          <div className="settings-avatar">{me[0].toUpperCase()}</div>
          <div>
            <div className="settings-username">@{me}</div>
            <div className="settings-subtitle">Account settings</div>
          </div>
          <button className="settings-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <nav className="settings-tabs">
          {(['profile', 'security', 'privacy', 'danger'] as Tab[]).map((t) => (
            <button key={t} className={`settings-tab ${tab === t ? 'active' : ''} ${t === 'danger' ? 'danger' : ''}`} onClick={() => setTab(t)}>
              {t === 'profile' && '👤 Profile'}
              {t === 'security' && '🔒 Security'}
              {t === 'privacy' && '🛡 Privacy'}
              {t === 'danger' && '⚠ Danger'}
            </button>
          ))}
        </nav>

        <div className="settings-body">
          {/* ── Profile ──────────────────────────────────────── */}
          {tab === 'profile' && (
            <div className="settings-section">
              <label className="settings-label">Display name</label>
              <input className="settings-input" placeholder={me} value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={64} />

              <label className="settings-label">Mood / status message</label>
              <input className="settings-input" placeholder="What's on your mind?" value={mood} onChange={(e) => setMood(e.target.value)} maxLength={140} />

              <label className="settings-label">Presence</label>
              <select className="settings-select" value={status} onChange={(e) => setStatus(e.target.value)}>
                <option value="Online">🟢 Online</option>
                <option value="Away">🟡 Away</option>
                <option value="Do Not Disturb">🔴 Do Not Disturb</option>
                <option value="Invisible">⚫ Invisible</option>
              </select>

              {profileMsg && <p className={`settings-msg ${profileMsg === 'Saved.' ? 'ok' : 'err'}`}>{profileMsg}</p>}
              <button className="settings-btn" onClick={saveProfile}>Save profile</button>
            </div>
          )}

          {/* ── Security ─────────────────────────────────────── */}
          {tab === 'security' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Change password</h3>
              <input className="settings-input" type="password" placeholder="Current password" value={oldPw} onChange={(e) => setOldPw(e.target.value)} autoComplete="current-password" />
              <input className="settings-input" type="password" placeholder="New password (8+ chars)" value={newPw} onChange={(e) => setNewPw(e.target.value)} autoComplete="new-password" />
              <input className="settings-input" type="password" placeholder="Confirm new password" value={newPw2} onChange={(e) => setNewPw2(e.target.value)} autoComplete="new-password" />
              {pwMsg && <p className={`settings-msg ${pwMsg === 'Password changed.' ? 'ok' : 'err'}`}>{pwMsg}</p>}
              <button className="settings-btn" onClick={changePassword}>Change password</button>

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Two-factor authentication (TOTP)</h3>
              {totpMsg && <p className={`settings-msg ${totpMsg.startsWith('2FA') ? 'ok' : 'err'}`}>{totpMsg}</p>}
              {totpUri && (
                <div className="settings-totp-uri">
                  <p className="settings-label">Copy this URI into your authenticator:</p>
                  <code className="settings-totp-code-block">{totpUri}</code>
                  <input className="settings-input" placeholder="Enter 6-digit code to confirm" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} inputMode="numeric" maxLength={6} />
                  <button className="settings-btn" onClick={confirmTotp}>Confirm 2FA</button>
                </div>
              )}
              {!totpPending && (
                <div className="settings-row">
                  <button className="settings-btn" onClick={enableTotp}>Enable 2FA</button>
                  <button className="settings-btn-secondary" onClick={disableTotp}>Disable 2FA</button>
                </div>
              )}
            </div>
          )}

          {/* ── Privacy ──────────────────────────────────────── */}
          {tab === 'privacy' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Blocked users</h3>
              {blockMsg && <p className="settings-msg ok">{blockMsg}</p>}
              {blocks.length === 0 ? (
                <p className="settings-empty">No blocked users.</p>
              ) : (
                <ul className="settings-block-list">
                  {blocks.map((u) => (
                    <li key={u} className="settings-block-item">
                      <span>{u}</span>
                      <button className="settings-btn-secondary small" onClick={() => unblock(u)}>Unblock</button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {/* ── Danger ───────────────────────────────────────── */}
          {tab === 'danger' && (
            <div className="settings-section">
              <h3 className="settings-section-title danger-title">Delete account</h3>
              <p className="settings-label">This permanently erases your account, all messages, friends, and encryption keys. <strong>Cannot be undone.</strong></p>
              <input className="settings-input" placeholder='Type "delete my account" to confirm' value={delConfirm} onChange={(e) => setDelConfirm(e.target.value)} />
              <input className="settings-input" type="password" placeholder="Your password" value={delPw} onChange={(e) => setDelPw(e.target.value)} autoComplete="current-password" />
              {delMsg && <p className="settings-msg err">{delMsg}</p>}
              <button
                className="settings-btn danger"
                onClick={deleteAccount}
                disabled={delConfirm !== 'delete my account' || !delPw}
              >
                Erase my account
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
