import type { ArtalkType } from 'artalk'
import { getArtalk } from './artalk'
import { session } from './session'

export interface ApiOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH'
  query?: Record<string, unknown>
  body?: unknown
  signal?: AbortSignal
}

export interface ApiResponse<T> {
  data: T
  msg?: string
}

/**
 * The widget's request client defaults to `text/plain`, which Fiber's body
 * parser rejects with 422. Every write from the console is JSON.
 */
const JSON_CONTENT_TYPE = 'application/json' as never

function bodyParams(body: unknown) {
  return body === undefined ? {} : { body, type: JSON_CONTENT_TYPE }
}

function compact(query?: Record<string, unknown>) {
  if (!query) return undefined
  const entries = Object.entries(query).filter(
    ([, value]) => value !== undefined && value !== null && value !== '',
  )
  return entries.length ? Object.fromEntries(entries) : undefined
}

/**
 * Call `/api/v2/*` through the Artalk client so the console inherits its base
 * URL, Authorization header, guest credential and error handling.
 */
export async function api<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const res = await getArtalk()
    .ctx.getApi()
    .request<T>({
      path,
      method: options.method ?? 'GET',
      query: compact(options.query),
      ...bodyParams(options.body),
      secure: true,
      format: 'json',
      signal: options.signal,
    })
  return res.data
}

/** Same as {@link api}, but keeps the server's `msg` for confirmation copy. */
export async function apiWithMessage<T>(path: string, options: ApiOptions = {}) {
  const res = await getArtalk()
    .ctx.getApi()
    .request<T & { msg?: string }>({
      path,
      method: options.method ?? 'GET',
      query: compact(options.query),
      ...bodyParams(options.body),
      secure: true,
      format: 'json',
      signal: options.signal,
    })
  return res.data
}

/** The signed-in user's site filter, spread into admin query strings. */
export function siteQuery(): { site_name?: string } {
  const site = session.get().site
  return site ? { site_name: site } : {}
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message
  if (typeof error === 'string') return error
  const data = (error as any)?.data
  if (data && typeof data === 'object' && typeof data.msg === 'string') return data.msg
  return ''
}

/**
 * The sidebar's admin endpoints return a few counters the widget's public types
 * do not declare, so they are widened here once instead of at every call site.
 */
export type PageData = ArtalkType.PageData & { comment_count?: number; pv?: number }
export type SiteData = ArtalkType.SiteData & { comment_count?: number }

// ===============
//  Rewards: points / badges / lucky uid
// ===============

export type BadgeGrantType = 'condition' | 'exchange' | 'manual'

export type BadgeConditionKind =
  | 'none'
  | 'comment_count'
  | 'vote_received'
  | 'register_days'
  | 'points'

export type BadgeSource = 'condition' | 'exchange' | 'manual' | 'import'

/** A badge definition as stored in the library (no ownership attached). */
export interface RewardsBadgeDefinition {
  id: number
  created_at: string
  updated_at: string
  key: string
  name: string
  color: string
  icon: string
  description: string
  grant_type: BadgeGrantType
  condition_kind: BadgeConditionKind
  condition_value: number
  price: number
  sort_order: number
  enabled: boolean
}

/** A badge definition plus the caller's ownership, as `/badges` returns it. */
export interface RewardsBadge extends RewardsBadgeDefinition {
  owned: boolean
  equipped: boolean
  source?: BadgeSource
  /** Condition badges only: whether the caller already meets the threshold. */
  achieved: boolean
  /** Condition badges only: current value, for the progress bar. */
  progress: number
  target: number
}

export interface RewardsBadgeList {
  badges: RewardsBadge[]
  is_login: boolean
  points: number
  uid: number
}

/** The payload accepted by both create and update. */
export interface BadgePayload {
  key: string
  name: string
  color?: string
  icon?: string
  description?: string
  grant_type: BadgeGrantType
  condition_kind?: BadgeConditionKind
  condition_value?: number
  price?: number
  sort_order?: number
  enabled?: boolean
}

export interface PointsOverview {
  uid: number
  points: number
  checked_in_today: boolean
  rules: {
    comment_create: number
    comment_create_daily_cap: number
    vote_received: number
    checkin: number
  }
  transactions: {
    id: number
    created_at: string
    amount: number
    balance: number
    reason: string
    ref_type: string
    ref_id: number
    note: string
  }[]
}

export interface CheckinResult {
  points: number
  awarded: number
  checked_in: boolean
}

export interface UidQuote {
  uid: number
  available: boolean
  is_lucky: boolean
  tier: '' | 'c' | 'b' | 'a' | 's'
  reason: string
  price: number
  message?: string
  current_uid: number
  points: number
  is_login: boolean
}

export interface UserBadgeList {
  badges: RewardsBadge[]
  points: number
  uid: number
}

/**
 * Endpoints behind the rewards system.
 *
 * Kept together instead of spread over the pages so the paths stay in one
 * place: they are the only part of this console that is not covered by the
 * generated API client.
 */
export const rewardsApi = {
  /** Badge wall for the signed-in user (or anonymous, with owned=false). */
  listBadges: () => api<RewardsBadgeList>('/badges'),

  /** Admin view: every badge, including disabled ones, without ownership. */
  adminList: () => api<{ badges: RewardsBadgeDefinition[] }>('/badges/all'),

  create: (payload: BadgePayload) => api<RewardsBadge>('/badges', { method: 'POST', body: payload }),
  update: (id: number, payload: BadgePayload) =>
    api<RewardsBadge>(`/badges/${id}`, { method: 'PUT', body: payload }),
  remove: (id: number) => api<{ msg: string }>(`/badges/${id}`, { method: 'DELETE' }),

  exchange: (id: number) => api<{ points: number }>(`/badges/${id}/exchange`, { method: 'POST' }),
  equip: (id: number) => api<{ msg: string }>(`/badges/${id}/equip`, { method: 'POST' }),
  unequip: () => api<{ msg: string }>('/badges/equip', { method: 'DELETE' }),

  myPoints: (limit = 50) => api<PointsOverview>('/points', { query: { limit } }),
  checkin: () => api<CheckinResult>('/points/checkin', { method: 'POST' }),

  quoteUid: (uid: number) => api<UidQuote>('/uid/quote', { query: { uid } }),
  exchangeUid: (uid: number) =>
    api<UidQuote & { previous_uid: number; current_uid: number; points: number }>('/uid/exchange', {
      method: 'POST',
      body: { uid },
    }),

  /** Per-user badge list for the admin user drawer. */
  userBadges: (userId: number) => api<UserBadgeList>(`/users/${userId}/badges`),
  adjustPoints: (userId: number, amount: number, note = '') =>
    api<{ points: number }>(`/users/${userId}/points`, {
      method: 'POST',
      body: { amount, note },
    }),
  grant: (userId: number, badgeId: number) =>
    api<{ msg: string }>(`/users/${userId}/badges/${badgeId}`, { method: 'POST' }),
  revoke: (userId: number, badgeId: number) =>
    api<{ msg: string }>(`/users/${userId}/badges/${badgeId}`, { method: 'DELETE' }),
}

