import {
  Button,
  Chip,
  Input,
  Label,
  Modal,
  Skeleton,
  Switch,
  TextField,
  toast,
} from '@heroui/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Award, FolderPlus, Pencil, Plus, Sparkles, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { EmptyState } from '@/components/ui/EmptyState'
import { PageHeader } from '@/components/ui/PageHeader'
import { Segmented } from '@/components/ui/Segmented'
import { useConfirm } from '@/components/ui/confirm'
import type {
  BadgeCategory,
  BadgeConditionKind,
  BadgeGrantType,
  CategoryPayload,
  RewardsBadgeDefinition,
} from '@/lib/api'
import { errorMessage, rewardsApi } from '@/lib/api'
import { t } from '@/lib/i18n'
import { CONDITIONS, GRANT_TYPES, conditionLabel, grantLabel, isImageIcon } from '@/lib/rewards-labels'
import { formatNumber } from '@/lib/utils'

const DEFAULT_BADGE_COLOR = '#a85f48'

interface BadgeGroup {
  category: BadgeCategory | null
  badges: RewardsBadgeDefinition[]
}

function groupByCategory(
  badges: RewardsBadgeDefinition[],
  categories: BadgeCategory[],
): BadgeGroup[] {
  const byCategory = new Map<number, RewardsBadgeDefinition[]>()
  for (const badge of badges) {
    const list = byCategory.get(badge.category_id) ?? []
    list.push(badge)
    byCategory.set(badge.category_id, list)
  }

  const groups: BadgeGroup[] = []
  for (const category of categories) {
    const list = byCategory.get(category.id)
    groups.push({ category, badges: list ?? [] })
    byCategory.delete(category.id)
  }

  const rest = [...byCategory.values()].flat()
  if (rest.length) groups.push({ category: null, badges: rest })
  return groups
}

/**
 * 徽章库管理。
 *
 * 徽章是部署数据而不是用户状态：管理员在这里定义"什么东西可以被拿到"，
 * 真正的发放发生在服务端——条件达标时自动发，或者用户花积分来换。
 */
export function BadgesPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [editor, setEditor] = useState<{ open: boolean; badge?: RewardsBadgeDefinition }>({
    open: false,
  })
  const [categoryEditor, setCategoryEditor] = useState<{
    open: boolean
    category?: BadgeCategory
  }>({ open: false })

  const query = useQuery({ queryKey: ['admin-badges'], queryFn: () => rewardsApi.adminList() })
  const badges = query.data?.badges ?? []
  const categories = query.data?.categories ?? []
  const groups = useMemo(() => groupByCategory(badges, categories), [badges, categories])

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['admin-badges'] })

  const seed = useMutation({
    mutationFn: () => rewardsApi.seedDefaults(),
    onSuccess: (result) => {
      toast.success(t('badgeSeeded', { categories: result.categories, badges: result.badges }))
      void refresh()
    },
    onError: (error) => toast.danger(errorMessage(error) || t('opFailed')),
  })

  const remove = useMutation({
    mutationFn: (id: number) => rewardsApi.remove(id),
    onSuccess: () => {
      toast.success(t('opSuccess'))
      void refresh()
    },
    onError: (error) => toast.danger(errorMessage(error) || t('opFailed')),
  })

  const removeCategory = useMutation({
    mutationFn: (id: number) => rewardsApi.removeCategory(id),
    onSuccess: (result) => {
      toast.success(t('categoryRemoved', { count: result.moved }))
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
          <div className="flex items-center gap-1.5">
            <Button
              size="sm"
              variant="ghost"
              isDisabled={seed.isPending}
              onPress={() => seed.mutate()}
            >
              <Sparkles className="size-4" aria-hidden />
              {t('seedBadges')}
            </Button>
            <Button size="sm" variant="ghost" onPress={() => setCategoryEditor({ open: true })}>
              <FolderPlus className="size-4" aria-hidden />
              {t('newCategory')}
            </Button>
            <Button size="sm" onPress={() => setEditor({ open: true })}>
              <Plus className="size-4" aria-hidden />
              {t('create')}
            </Button>
          </div>
        }
      />

      {query.isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-16 rounded-xl" />
          ))}
        </div>
      ) : badges.length ? (
        <div className="space-y-4">
          {groups.map((group) => (
            <section key={group.category?.id ?? '__uncategorised__'}>
              <div className="mb-1.5 flex items-center gap-1.5">
                <h2 className="text-[11px] font-medium uppercase tracking-[0.1em] text-muted">
                  {group.category?.name ?? t('badgeUncategorized')}
                </h2>
                <span className="text-[10.5px] tabular-nums text-muted/70">
                  {group.badges.length}
                </span>
                {group.category ? (
                  <>
                    {!group.category.enabled ? (
                      <Chip size="sm" variant="soft" className="text-danger">
                        {t('badgeDisabled')}
                      </Chip>
                    ) : null}
                    <Button
                      size="sm"
                      variant="ghost"
                      isIconOnly
                      aria-label={t('edit')}
                      onPress={() => setCategoryEditor({ open: true, category: group.category! })}
                    >
                      <Pencil className="size-3.5" aria-hidden />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      isIconOnly
                      className="text-danger"
                      aria-label={t('delete')}
                      isDisabled={removeCategory.isPending}
                      onPress={async () => {
                        const ok = await confirm({ title: group.category!.name, danger: true })
                        if (!ok) return
                        removeCategory.mutate(group.category!.id)
                      }}
                    >
                      <Trash2 className="size-3.5" aria-hidden />
                    </Button>
                  </>
                ) : null}
              </div>

              {group.badges.length ? (
                <div className="overflow-hidden rounded-xl border border-border bg-surface">
                  {group.badges.map((badge, index) => (
                    <div
                      key={badge.id}
                      className={`flex items-center gap-3 px-3 py-2.5 md:px-4 ${
                        index ? 'border-t border-border' : ''
                      }`}
                    >
                      <span
                        className="grid size-8 shrink-0 place-items-center overflow-hidden rounded-lg text-[13px] font-semibold"
                        style={{
                          color: badge.color || DEFAULT_BADGE_COLOR,
                          backgroundColor: `${badge.color || DEFAULT_BADGE_COLOR}1f`,
                        }}
                        aria-hidden
                      >
                        {badge.icon ? (
                          isImageIcon(badge.icon) ? (
                            <img src={badge.icon} alt="" className="size-full object-cover" />
                          ) : (
                            badge.icon
                          )
                        ) : (
                          <Award className="size-4" />
                        )}
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
                <p className="rounded-xl border border-dashed border-border px-3 py-2 text-[11.5px] text-muted">
                  {t('badgeCategoryEmpty')}
                </p>
              )}
            </section>
          ))}
        </div>
      ) : (
        <div className="space-y-3">
          <EmptyState icon={Award} title={t('badgeLibrary')} />
          <div className="rounded-xl border border-dashed border-border p-4 text-center">
            <p className="text-[12.5px] text-muted">{t('seedBadgesHint')}</p>
            <Button
              size="sm"
              className="mt-2"
              isDisabled={seed.isPending}
              onPress={() => seed.mutate()}
            >
              <Sparkles className="size-4" aria-hidden />
              {t('seedBadges')}
            </Button>
          </div>
        </div>
      )}

      <BadgeEditor
        isOpen={editor.open}
        badge={editor.badge}
        categories={categories}
        onClose={() => setEditor({ open: false })}
        onSaved={refresh}
      />

      <CategoryEditor
        isOpen={categoryEditor.open}
        category={categoryEditor.category}
        onClose={() => setCategoryEditor({ open: false })}
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
  category_id: number
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
    category_id: badge?.category_id ?? 0,
    grant_type: badge?.grant_type ?? 'manual',
    condition_kind:
      badge?.condition_kind && badge.condition_kind !== 'none'
        ? badge.condition_kind
        : 'comment_count',
    condition_value: badge?.condition_value ? String(badge.condition_value) : '1',
    price: badge?.price ? String(badge.price) : '0',
    sort_order: badge?.sort_order ? String(badge.sort_order) : '0',
    enabled: badge?.enabled ?? true,
  }
}

function BadgeEditor({
  isOpen,
  badge,
  categories,
  onClose,
  onSaved,
}: {
  isOpen: boolean
  badge?: RewardsBadgeDefinition
  categories: BadgeCategory[]
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
                categories={categories}
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
  categories,
  onClose,
  onSaved,
}: {
  badge?: RewardsBadgeDefinition
  categories: BadgeCategory[]
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
        category_id: form.category_id,
        grant_type: form.grant_type,
        // 非条件徽章不写条件，免得库里留下一条用不到的阈值
        condition_kind: needsCondition ? form.condition_kind : ('none' as const),
        condition_value: needsCondition ? Number(form.condition_value) : 0,
        price: Number(form.price) || 0,
        sort_order: Number(form.sort_order) || 0,
        enabled: form.enabled,
      }
      if (isCreate) await rewardsApi.create(payload)
      else await rewardsApi.update(badge.id, payload)
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

        <div className="col-span-2">
          <Label className="text-[12px] text-muted">{t('badgeCategory')}</Label>
          <select
            value={form.category_id}
            onChange={(event) => set('category_id', Number(event.target.value))}
            className="mt-1 w-full rounded-lg border border-border bg-surface px-2 py-1.5 text-[12.5px]"
          >
            <option value={0}>{t('badgeUncategorized')}</option>
            {categories.map((category) => (
              <option key={category.id} value={category.id}>
                {category.name}
              </option>
            ))}
          </select>
        </div>

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
          <Label className="text-[12px] text-muted">icon</Label>
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

function CategoryEditor({
  isOpen,
  category,
  onClose,
  onSaved,
}: {
  isOpen: boolean
  category?: BadgeCategory
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
              <Modal.Heading>{category ? t('edit') : t('newCategory')}</Modal.Heading>
            </Modal.Header>
            {isOpen ? (
              <CategoryForm
                key={category?.id ?? 'new'}
                category={category}
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

function CategoryForm({
  category,
  onClose,
  onSaved,
}: {
  category?: BadgeCategory
  onClose: () => void
  onSaved: () => void
}) {
  const isCreate = !category
  const [form, setForm] = useState<CategoryPayload>(() => ({
    key: category?.key ?? '',
    name: category?.name ?? '',
    sort_order: category?.sort_order ?? 0,
    enabled: category?.enabled ?? true,
  }))
  const [saving, setSaving] = useState(false)

  const submit = async () => {
    if (!form.name?.trim()) {
      toast.danger(t('badgeName'))
      return
    }
    if (!/^[a-z0-9][a-z0-9_-]{1,63}$/.test(form.key.trim())) {
      toast.danger(t('categoryKeyHint'))
      return
    }

    setSaving(true)
    try {
      const payload: CategoryPayload = {
        key: form.key.trim(),
        name: form.name.trim(),
        sort_order: Number(form.sort_order) || 0,
        enabled: form.enabled,
      }
      if (isCreate) await rewardsApi.createCategory(payload)
      else await rewardsApi.updateCategory(category.id, payload)
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
      <TextField isRequired>
        <Label className="text-[12px] text-muted">{t('badgeName')}</Label>
        <Input
          value={form.name}
          onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
          autoComplete="off"
          placeholder={t('newCategory')}
        />
      </TextField>

      <TextField isRequired>
        <Label className="text-[12px] text-muted">key</Label>
        <Input
          value={form.key}
          onChange={(event) => setForm((prev) => ({ ...prev, key: event.target.value }))}
          autoComplete="off"
          disabled={!isCreate}
          placeholder="growth"
          className="font-mono text-[12px]"
        />
        <p className="mt-1 text-[11px] text-muted">{t('categoryKeyHint')}</p>
      </TextField>

      <TextField>
        <Label className="text-[12px] text-muted">sort</Label>
        <Input
          type="number"
          value={String(form.sort_order ?? 0)}
          onChange={(event) =>
            setForm((prev) => ({ ...prev, sort_order: Number(event.target.value) || 0 }))
          }
        />
      </TextField>

      <Switch
        isSelected={form.enabled ?? true}
        onChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))}
      >
        <Switch.Content>
          <Switch.Control>
            <Switch.Thumb />
          </Switch.Control>
          {t('enabled')}
        </Switch.Content>
      </Switch>

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
