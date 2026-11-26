import type { BadgeConditionKind, BadgeGrantType } from './api'
import { t } from './i18n'

/**
 * 徽章枚举的展示文案。
 *
 * 管理页和"我的徽章"页都要把 grant_type / condition_kind 翻成人话，
 * 集中在这里改一次就够了。
 */
export const GRANT_TYPES: { id: BadgeGrantType; label: () => string }[] = [
  { id: 'condition', label: () => t('grantCondition') },
  { id: 'exchange', label: () => t('grantExchange') },
  { id: 'manual', label: () => t('grantManual') },
]

export const CONDITIONS: { id: BadgeConditionKind; label: () => string }[] = [
  { id: 'comment_count', label: () => t('condCommentCount') },
  { id: 'vote_received', label: () => t('condVoteReceived') },
  { id: 'register_days', label: () => t('condRegisterDays') },
  { id: 'points', label: () => t('condPoints') },
]

export function grantLabel(type: BadgeGrantType): string {
  return GRANT_TYPES.find((item) => item.id === type)?.label() ?? type
}

export function conditionLabel(kind: BadgeConditionKind): string {
  return CONDITIONS.find((item) => item.id === kind)?.label() ?? kind
}

/** 徽章图标可能是 emoji、单个字符，也可能是一个图片 URL。 */
export function isImageIcon(icon: string): boolean {
  return /^(https?:\/\/|\/|data:image\/)/.test(icon)
}
