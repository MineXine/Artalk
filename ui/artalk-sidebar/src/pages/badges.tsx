import { Button, Chip, Input, Label, Modal, Skeleton, Switch, TextField, toast } from '@heroui/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Award, Plus } from 'lucide-react'
import { useState } from 'react'
import { EmptyState } from '@/components/ui/EmptyState'
import { PageHeader } from '@/components/ui/PageHeader'
import { Segmented } from '@/components/ui/Segmented'
import { useConfirm } from '@/components/ui/confirm'
import type { BadgeConditionKind, BadgeGrantType, RewardsBadgeDefinition } from '@/lib/api'
import { errorMessage, rewardsApi } from '@/lib/api'
import { t } from '@/lib/i18n'
import { formatNumber } from '@/lib/utils'

const DEFAULT_BADGE_COLOR = '#a85f48'

const GRANT_TYPES: { id: BadgeGrantType; label: () => string }[] = [
  { id: 'condition', label: () => t('grantCondition') },
  { id: 'exchange', label: () => t('grantExchange') },
  { id: 'manual', label: () => t('grantManual') },
]

const CONDITIONS: { id: BadgeConditionKind; label: () => string }[] = [
  { id: 'comment_count', label: () => t('condCommentCount') },
  { id: 'vote_received', label: () => t('condVoteReceived') },
  { id: 'register_days', label: () => t('condRegisterDays') },
  { id: 'points', label: () => t('condPoints') },
]

function grantLabel(type: BadgeGrantType): string {
  return GRANT_TYPES.find((item) => item.id === type)?.label() ?? type
}

function conditionLabel(kind: BadgeConditionKind): string {
  return CONDITIONS.find((item) => item.id === kind)?.label() ?? kind
}

/**
 * Badge library management.
 *
 * The library is deployment data, not per-user state: an admin defines what can
 * be earned, and the granting itself happens on the server — by condition when a
 * comment is created, or by exchange when a user spends points.
 */
export function BadgesPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [editor, setEditor] = useState<{ open: boolean; badge?: RewardsBadgeDefinition }>({
    open: false,
  })

  const query = useQuery({
    queryKey: ['admin-badges'],
    queryFn: () => rewardsApi.adminList(),
  })
  const badges = query.data?.badges ?? []

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['admin-badges'] })

  const remove = useMutation({
    mutationFn: (id: number) => rewardsApi.remove(id),
    onSuccess: () => {
      toast.success(t('settingSaved'))
      void refresh()
    },
    onError: (error) => toast.danger(errorMessage(error) || t('opFailed')),
  })

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('badgeLibrary')}
        description={t('badgeLibraryDesc')}
        actions={
          <Button size="sm" onPress={() => setEditor({ open: true })}>
            <Plus className="size-4" aria-hidden />
            {t('create')}
          </Button>
        }
      />

      {query.isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-16 rounded-xl" />
          ))}
        </div>
      ) : badges.length ? (
        <div className="overflow-hidden rounded-xl border border-border bg-surface">
          {badges.map((badge, index) => (
            <div
              key={badge.id}
              className={`flex items-center gap-3 px-3 py-2.5 md:px-4 ${
                index ? 'border-t border-border' : ''
              }`}
            >
              <span
                className="grid size-8 shrink-0 place-items-center rounded-lg text-[13px] font-semibold"
                style={{
                  color: badge.color || DEFAULT_BADGE_COLOR,
                  backgroundColor: `${badge.color || DEFAULT_BADGE_COLOR}1f`,
                }}
                aria-hidden
              >
                {badge.icon || <Award className="size-4" />}
              </span>

              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="truncate text-[13px] font-medium">{badge.name}</span>
                  <Chip size="sm" variant="soft">
                    {grantLabel(badge.grant_type)}
                  </Chip>
                  {!badge.enabled ? (
                    <Chip size="sm" variant="soft" className="text-danger">
                      {t('badgeDisabled')}
                    </Chip>
                  ) : null}
                </div>
                <p className="truncate font-mono text-[11px] text-muted">
                  {badge.key}
                  {badge.grant_type === 'condition'
                    ? ` · ${conditionLabel(badge.condition_kind)} ≥ ${formatNumber(badge.condition_value)}`
                    : ''}
                  {badge.price > 0 ? ` · ${formatNumber(badge.price)} ${t('points')}` : ''}
                </p>
              </div>

              <div className="flex shrink-0 items-center gap-0.5">
                <Button
                  size="sm"
                  variant="ghost"
                  onPress={() => setEditor({ open: true, badge })}
                >
                  {t('edit')}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-danger"
                  isDisabled={remove.isPending}
                  onPress={async () => {
                    const ok = await confirm({ title: badge.name, danger: true })
                    if (!ok) return
                    remove.mutate(badge.id)
                  }}
                >
                  {t('delete')}
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <EmptyState icon={Award} title={t('badgeLibrary')} />
      )}

      <BadgeEditor
        isOpen={editor.open}
        badge={editor.badge}
        onClose={() => setEditor({ open: false })}
        onSaved={refresh}
      />
    </div>
  )
}

interface BadgeForm {
  key: string
  name: string
  color: string
  icon: string
  description: string
  grant_type: BadgeGrantType
  condition_kind: BadgeConditionKind
  condition_value: string
  price: string
  sort_order: string
  enabled: boolean
}

function toForm(badge?: RewardsBadgeDefinition): BadgeForm {
  return {
    key: badge?.key ?? '',
    name: badge?.name ?? '',
    color: badge?.color ?? DEFAULT_BADGE_COLOR,
    icon: badge?.icon ?? '',
    description: badge?.description ?? '',
    grant_type: badge?.grant_type ?? 'manual',
    condition_kind:
      badge?.condition_kind && badge.condition_kind !== 'none' ? badge.condition_kind : 'comment_count',
    condition_value: badge?.condition_value ? String(badge.condition_value) : '1',
    price: badge?.price ? String(badge.price) : '0',
    sort_order: badge?.sort_order ? String(badge.sort_order) : '0',
    enabled: badge?.enabled ?? true,
  }
}

function BadgeEditor({
  isOpen,
  badge,
  onClose,
  onSaved,
}: {
  isOpen: boolean
  badge?: RewardsBadgeDefinition
  onClose: () => void
  onSaved: () => void
}) {
  return (
    <Modal isOpen={isOpen} onOpenChange={(open) => !open && onClose()}>
      <Modal.Backdrop>
        <Modal.Container>
          <Modal.Dialog className="gap-3">
            <Modal.CloseTrigger aria-label={t('close')} />
            <Modal.Header>
              <Modal.Heading>{badge ? t('edit') : t('create')}</Modal.Heading>
            </Modal.Header>
            {isOpen ? (
              <BadgeForm
                key={badge?.id ?? 'new'}
                badge={badge}
                onClose={onClose}
                onSaved={onSaved}
              />
            ) : null}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}

function BadgeForm({
  badge,
  onClose,
  onSaved,
}: {
  badge?: RewardsBadgeDefinition
  onClose: () => void
  onSaved: () => void
}) {
  const isCreate = !badge
  const [form, setForm] = useState<BadgeForm>(() => toForm(badge))
  const [saving, setSaving] = useState(false)

  const set = <K extends keyof BadgeForm>(key: K, value: BadgeForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }))

  const needsCondition = form.grant_type === 'condition'

  const submit = async () => {
    if (!form.name.trim()) {
      toast.danger(t('badgeName'))
      return
    }
    if (!/^[a-z0-9][a-z0-9_-]{1,63}$/.test(form.key.trim())) {
      toast.danger(t('badgeKeyHint'))
      return
    }
    if (needsCondition && Number(form.condition_value) <= 0) {
      toast.danger(t('badgeThreshold'))
      return
    }

    setSaving(true)
    try {
      const payload = {
        key: form.key.trim(),
        name: form.name.trim(),
        color: form.color.trim(),
        icon: form.icon.trim(),
        description: form.description.trim(),
        grant_type: form.grant_type,
        // 非条件徽章不写条件，避免后端把一条无用阈值存进库
        condition_kind: needsCondition ? form.condition_kind : ('none' as const),
        condition_value: needsCondition ? Number(form.condition_value) : 0,
        price: Number(form.price) || 0,
        sort_order: Number(form.sort_order) || 0,
        enabled: form.enabled,
      }
      if (isCreate) {
        await rewardsApi.create(payload)
      } else {
        await rewardsApi.update(badge.id, payload)
      }
      onSaved()
      onClose()
    } catch (error) {
      toast.danger(errorMessage(error) || t('opFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      className="min-h-0 space-y-3 overflow-y-auto px-1 pb-1"
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
    >
      <div className="grid grid-cols-2 gap-2">
        <TextField isRequired className="col-span-2">
          <Label className="text-[12px] text-muted">{t('badgeName')}</Label>
          <Input
            value={form.name}
            onChange={(event) => set('name', event.target.value)}
            autoComplete="off"
          />
        </TextField>

        <TextField isRequired className="col-span-2">
          <Label className="text-[12px] text-muted">{t('badgeGrantType')} · key</Label>
          <Input
            value={form.key}
            onChange={(event) => set('key', event.target.value)}
            autoComplete="off"
            disabled={!isCreate}
            placeholder="first-comment"
            className="font-mono text-[12px]"
          />
          <p className="mt-1 text-[11px] text-muted">{t('badgeKeyHint')}</p>
        </TextField>

        <TextField className="col-span-2">
          <Label className="text-[12px] text-muted">{t('badgeDescription')}</Label>
          <Input
            value={form.description}
            onChange={(event) => set('description', event.target.value)}
            autoComplete="off"
          />
        </TextField>

        <TextField>
          <Label className="text-[12px] text-muted">{t('badgeColor')}</Label>
          <div className="mt-1 flex items-center gap-1.5">
            <span className="relative size-9 shrink-0 overflow-hidden rounded-lg border border-border">
              <input
                type="color"
                aria-label={t('badgeColor')}
                value={form.color || DEFAULT_BADGE_COLOR}
                onChange={(event) => set('color', event.target.value)}
                className="absolute -inset-2 size-[calc(100%+16px)] cursor-pointer border-0 bg-transparent p-0"
              />
            </span>
            <Input
              value={form.color}
              onChange={(event) => set('color', event.target.value)}
              placeholder={DEFAULT_BADGE_COLOR}
              className="font-mono text-[12px]"
            />
          </div>
        </TextField>

        <TextField>
          <Label className="text-[12px] text-muted">{t('badgeDescription')} · icon</Label>
          <Input
            value={form.icon}
            onChange={(event) => set('icon', event.target.value)}
            autoComplete="off"
            placeholder="⭐ or https://"
          />
        </TextField>

        <div className="col-span-2">
          <Label className="text-[12px] text-muted">{t('badgeGrantType')}</Label>
          <Segmented
            className="mt-1 w-full"
            ariaLabel={t('badgeGrantType')}
            items={GRANT_TYPES.map((item) => ({ id: item.id, label: item.label() }))}
            value={form.grant_type}
            onChange={(next) => set('grant_type', next)}
          />
        </div>

        {needsCondition ? (
          <>
            <div className="col-span-2">
              <Label className="text-[12px] text-muted">{t('badgeCondition')}</Label>
              <Segmented
                className="mt-1 w-full"
                ariaLabel={t('badgeCondition')}
                items={CONDITIONS.map((item) => ({ id: item.id, label: item.label() }))}
                value={form.condition_kind}
                onChange={(next) => set('condition_kind', next)}
              />
            </div>
            <TextField className="col-span-2">
              <Label className="text-[12px] text-muted">{t('badgeThreshold')}</Label>
              <Input
                type="number"
                min={1}
                value={form.condition_value}
                onChange={(event) => set('condition_value', event.target.value)}
              />
            </TextField>
          </>
        ) : null}

        <TextField>
          <Label className="text-[12px] text-muted">{t('badgePrice')}</Label>
          <Input
            type="number"
            min={0}
            value={form.price}
            onChange={(event) => set('price', event.target.value)}
          />
          <p className="mt-1 text-[11px] text-muted">{t('points')}</p>
        </TextField>

        <TextField>
          <Label className="text-[12px] text-muted">sort</Label>
          <Input
            type="number"
            value={form.sort_order}
            onChange={(event) => set('sort_order', event.target.value)}
          />
        </TextField>

        <div className="col-span-2">
          <Switch isSelected={form.enabled} onChange={(value) => set('enabled', value)}>
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
              {t('enabled')}
            </Switch.Content>
          </Switch>
        </div>
      </div>

      <div className="flex justify-end gap-2 pt-1">
        <Button size="sm" variant="ghost" type="button" onPress={onClose}>
          {t('cancel')}
        </Button>
        <Button size="sm" type="submit" isDisabled={saving}>
          {t('save')}
        </Button>
      </div>
    </form>
  )
}
