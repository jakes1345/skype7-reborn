/** Mirrors `NexusMessage` in nexus_server/main.go (JSON field names). */

export interface TurnConfig {
  url: string
  username: string
  password: string
}

export interface NexusMessage {
  type: string
  sender?: string
  recipient?: string
  body?: string
  status?: string
  results?: string[]
  sdp?: string
  candidate?: string
  token?: string
  error?: string
  email?: string
  mood?: string
  display_name?: string
  convo_id?: string
  convo_name?: string
  members?: string[]
  turn_config?: TurnConfig
  totp_code?: string
  totp_uri?: string
  qr_token?: string
  qr_data?: string
  device_info?: string
  envelopes?: Record<string, string>
  /** Go JSON encodes []byte as base64 string */
  public_key?: string | number[]
  key_fingerprint?: string

  // ---- Servers + Channels ("Spaces") ----
  server_id?: string
  channel_id?: string
  server_name?: string
  channel_name?: string
  topic?: string
  kind?: 'text' | 'voice'
  role?: 'owner' | 'admin' | 'member'
  visibility?: 'public' | 'private'
  invite_code?: string
  servers?: ServerSummary[]
  channels?: ChannelInfo[]
  messages?: ChannelMsg[]
  history_from?: number
}

export interface ServerSummary {
  id: string
  name: string
  description?: string
  icon?: string
  owner: string
  visibility: 'public' | 'private'
  role: 'owner' | 'admin' | 'member'
  invite_code?: string
}

export interface ChannelInfo {
  id: string
  server_id: string
  name: string
  topic?: string
  kind: 'text' | 'voice'
  position: number
}

export interface ChannelMsg {
  id: number
  channel_id: string
  sender: string
  body: string
  created_at: string
}
