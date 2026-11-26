import { t } from './i18n'
import type { OptionNode } from './settings-option'

/**
 * 系统设置里不出现的配置根节点。
 *
 * `security` 与 `captcha` 由「安全防护」页面接管（含验证码服务商密钥），
 * `console` 有独立的「站点配置」页面，`admin_users` 走账号管理——在设置页
 * 重复列一遍只会多出一个容易改错的地方。
 */
export const HIDDEN_ROOT_NODES = new Set(['admin_users', 'console', 'security', 'captcha'])

/**
 * 把配置树整理成设置页左侧的索引分组。
 *
 * 没有子节点的根项（app_key、debug 这类单字段）会被压平合并成一个「配置文件」
 * 分组，否则索引里会堆满只有一项的分类；有子节点的根项各自成为一个分组。
 *
 * 抽成纯函数是为了可测试：索引里能不能看到某个分区，取决于这里有没有把它
 * 过滤掉，而这正是"配置项明明写了却看不见"最常见的原因。
 */
export function buildRootGroups(tree: OptionNode): OptionNode[] {
  const visible = (tree.items ?? []).filter((node) => !HIDDEN_ROOT_NODES.has(node.name))
  const leaves = visible.filter((node) => !node.items?.length).map((node) => ({ ...node, level: 2 }))

  const groups = visible.filter((node) => !!node.items?.length)

  if (leaves.length) {
    groups.unshift({
      name: '__basic__',
      path: '__basic__',
      level: 1,
      type: 'object',
      title: t('config'),
      items: leaves,
    })
  }

  return groups
}
