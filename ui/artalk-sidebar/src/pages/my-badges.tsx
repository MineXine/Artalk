import { Button, Chip, Skeleton, toast } from '@heroui/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Award, CheckCircle2 } from 'lucide-react'
import { useMemo } from 'react'
import { EmptyState } from '@/components/ui/EmptyState'
import { PageHeader } from '@/components/ui/PageHeader'
import type { BadgeCategory, RewardsBadge } from '@/lib/api'
import { errorMessage, rewardsApi } from '@/lib/api'
import { t } from '@/lib/i18n'
import { isImageIcon } from '@/lib/rewards-labels'
import { cn, formatNumber } from '@/lib/utils'

interface BadgeGroup {
  key: string
  name: string
  badges: RewardsBadge[]
}

/**
 * 按分类分组，未分类的排在最后。
 *
 * 分类被删除后它下面的徽章回到 category_id = 0，所以"未分类"不是异常状态，
 * 而是正常的一种归属，必须给它一个分组，否则那些徽章会从墙上消失。
 */
function groupBadges(badges: RewardsBadge[], categories: BadgeCategory[]): BadgeGroup[] {
  const byCategory = new Map<number, RewardsBadge[]>()
  for (const badge of badges) {
    const list = byCategory.get(badge.category_id) ?? []
    list.push(badge)
    byCategory.set(badge.category_id, list)
  }

  const groups: BadgeGroup[] = []
  for (const category of categories) {
    const list = byCategory.get(category.id)
    if (list?.length) {
      groups.push({ key: category.key, name: category.name, badges: list })
      byCategory.delete(category.id)
    }
  }

  const rest = [...byCategory.values()].flat()
  if (rest.length) {
    groups.push({ key: '__uncategorised__', name: t('badgeUncategorized'), badges: rest })
  }
  return groups
}

function BadgeAvatar({ badge, muted }: { badge: RewardsBadge; muted: boolean }) {
  const color = badge.color || '#a85f48'
  return (
    <span
      className={cn(
        'grid size-10 shrink-0 place-items-center overflow-hidden rounded-xl text-[18px] leading-none',
        muted && 'grayscale opacity-50',
      )}
      style={{ color, backgroundColor: `${color}1f` }}
      aria-hidden
    >
      {badge.icon ? (
        isImageIcon(badge.icon) ? (
          <img src={badge.icon} alt="" className="size-full object-cover" />
        ) : (
          badge.icon
        )
      ) : (
        <Award className="size-5" />
      )}
    </span>
  )
}

function BadgeCard({
  badge,
  points,
  busy,
  onEquip,
  onUnequip,
  onExchange,
}: {
  badge: RewardsBadge
  points: number
  busy: boolean
  onEquip: () => void
  onUnequip: () => void
  onExchange: () => void
}) {
  const isExchange = badge.grant_type === 'exchange'
  const canBuy = isExchange && !badge.owned && badge.price > 0
  const affordable = points >= badge.price
  const percent =
    badge.target > 0 ? Math.min(100, Math.round((badge.progress / badge.target) * 100)) : 0

  return (
    <div
      className={cn(
        'rounded-xl border p-3',
        badge.owned ? 'border-border bg-surface' : 'border-border/60 bg-surface-secondary/30',
      )}
    >
      <div className="flex items-start gap-2.5">
        <BadgeAvatar badge={badge} muted={!badge.owned} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span
              className={cn('truncate text-[13px] font-medium', !badge.owned && 'text-muted')}
            >
              {badge.name}
            </span>
            {badge.equipped ? (
              <Chip size="sm" variant="soft" color="accent">
                {t('badgeEquipped')}
              </Chip>
            ) : null}
          </div>
          {badge.description ? (
            <p className="mt-0.5 text-[11.5px] leading-snug text-muted">{badge.description}</p>
          ) : null}
        </div>
      </div>

      {!badge.owned && badge.target > 0 ? (
        <div className="mt-2">
          <div className="h-1 w-full overflow-hidden rounded-full bg-border">
            <div
              className={cn('h-full rounded-full', badge.achieved ? 'bg-accent' : 'bg-accent/50')}
              style={{ width: `${percent}%` }}
            />
          </div>
          <p className="mt-1 text-[10.5px] tabular-nums text-muted">
            {formatNumber(badge.progress)} / {formatNumber(badge.target)}
          </p>
        </div>
      ) : null}

      <div className="mt-2 flex items-center justify-end gap-1.5">
        {badge.owned ? (
          badge.equipped ? (
            <Button size="sm" variant="ghost" isDisabled={busy} onPress={onUnequip}>
              {t('badgeUnequip')}
            </Button>
          ) : (
            <Button size="sm" variant="secondary" isDisabled={busy} onPress={onEquip}>
              {t('badgeEquip')}
            </Button>
          )
        ) : canBuy ? (
          <Button size="sm" isDisabled={!affordable || busy} onPress={onExchange}>
            {t('badgeExchangeFor', { price: formatNumber(badge.price) })}
          </Button>
        ) : badge.grant_type === 'manual' ? (
          <span className="text-[10.5px] text-muted">{t('badgeManualOnly')}</span>
        ) : null}
      </div>
    </div>
  )
}

/**
 * 用户侧的徽章墙。
 *
 * 与 /badges 的管理视图共享同一个路由，靠 session 的 view 区分：管理员在
 * "我的"视角下看到的也是这一页，而不是自己的管理界面。
 */
export function MyBadgesPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const wall = useQuery({ queryKey: ['my-badges'], queryFn: () => rewardsApi.listBadges() })
  const points = useQuery({ queryKey: ['my-points'], queryFn: () => rewardsApi.myPoints(20) })

  const data = wall.data
  const groups = useMemo(
    () => groupBadges(data?.badges ?? [], data?.categories ?? []),
    [data?.badges, data?.categories],
  )

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['my-badges'] })
    void queryClient.invalidateQueries({ queryKey: ['my-points'] })
  }

  const equip = useMutation({
    mutationFn: (id: number) => rewardsApi.equip(id),
    onSuccess: () => {
      toast.success(t('opSuccess'))
      refresh()
    },
    onError: (error) => toast.danger(errorMessage(error) || t('opFailed')),
  })

  const unequip = useMutation({
    mutationFn: () => rewardsApi.unequip(),
    onSuccess: () => {
      toast.success(t('opSuccess'))
      refresh()
    },
    onError: (error) => toast.danger(errorMessage(error) || t('opFailed')),
  })

  const exchange = useMutation({
    mutationFn: (id: number) => rewardsApi.exchange(id),
    onSuccess: () => {
      toast.success(t('opSuccess'))
      refresh()
    },
    onError: (error) => toast.danger(errorMessage(error) || t('opFailed')),
  })

  const checkin = useMutation({
    mutationFn: () => rewardsApi.checkin(),
    onSuccess: (result) => {
      if (result.checked_in) toast.success(t('checkinReward', { points: result.awarded }))
      else toast.info(t('checkinDone'))
      refresh()
    },
    onError: (error) => toast.danger(errorMessage(error) || t('opFailed')),
  })

  const busy = equip.isPending || unequip.isPending || exchange.isPending
  const checkedIn = points.data?.checked_in_today ?? false

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('navMyBadges')}
        description={t('badgeWallDesc')}
        actions={
          data?.is_login ? (
            <Button
              size="sm"
              variant={checkedIn ? 'ghost' : 'primary'}
              isDisabled={checkedIn || checkin.isPending}
              onPress={() => checkin.mutate()}
            >
              {checkedIn ? (
                <>
                  <CheckCircle2 className="size-3.5" aria-hidden />
                  {t('checkinDone')}
                </>
              ) : (
                t('checkinAction')
              )}
            </Button>
          ) : null
        }
      />

      {/* 门牌号与积分：徽章墙的"我在哪儿"，改名字改邮箱都不会影响这两个值 */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 rounded-xl border border-border bg-surface px-3 py-2.5">
        <span className="text-[12px] text-muted">
          {t('uid')}
          <span className="ml-1.5 font-mono text-[13px] font-medium text-foreground">
            #{data?.uid || '—'}
          </span>
        </span>
        <span aria-hidden className="hidden h-3 w-px bg-border md:block" />
        <span className="text-[12px] text-muted">
          {t('points')}
          <span className="ml-1.5 text-[13px] font-medium tabular-nums text-foreground">
            {formatNumber(data?.points ?? 0)}
          </span>
        </span>
        {data && !data.is_login ? (
          <Button size="sm" variant="ghost" onPress={() => void navigate({ to: '/login' })}>
            {t('login')}
          </Button>
        ) : null}
      </div>

      {data && !data.is_login ? (
        <p className="rounded-xl border border-dashed border-border px-3 py-2 text-[12px] text-muted">
          {t('badgeLoginHint')}
        </p>
      ) : null}

      {wall.isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : groups.length ? (
        <div className="space-y-4">
          {groups.map((group) => (
            <section key={group.key}>
              <h2 className="mb-2 text-[11px] font-medium uppercase tracking-[0.1em] text-muted">
                {group.name}
              </h2>
              <div className="space-y-2">
                {group.badges.map((badge) => (
                  <BadgeCard
                    key={badge.id}
                    badge={badge}
                    points={data?.points ?? 0}
                    busy={busy}
                    onEquip={() => equip.mutate(badge.id)}
                    onUnequip={() => unequip.mutate()}
                    onExchange={() => exchange.mutate(badge.id)}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      ) : (
        <EmptyState icon={Award} title={t('badgeEmpty')} />
      )}
    </div>
  )
}
