import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import YAML from 'yaml'
import { describe, expect, it } from 'vitest'
import settings from './settings'
import type { OptionNode } from './settings-option'
import { buildRootGroups } from './settings-sections'

/**
 * 系统设置页是「模板驱动」的：它把 conf/artalk.example.<locale>.yml 解析成一棵
 * 选项树，键名和注释就是界面上显示的文字。所以这块配置能不能用，取决于模板
 * 长什么样，而不是有没有某个前端组件。
 *
 * 这些断言直接读真实的模板文件，验证管理员最终看到的那几个标题。
 */
function templatePath(name: string): string {
  // vitest 的 cwd 取决于从哪儿启动（包目录或仓库根），两个位置都试一下。
  const candidates = [
    resolve(process.cwd(), '../../conf', name),
    resolve(process.cwd(), 'conf', name),
  ]
  const found = candidates.find((candidate) => existsSync(candidate))
  if (!found) throw new Error(`template not found, tried: ${candidates.join(', ')}`)
  return found
}

function loadTree(name: string): OptionNode {
  return settings.init(YAML.parseDocument(readFileSync(templatePath(name), 'utf8'))).getTree()
}

function findNode(root: OptionNode, path: string[]): OptionNode | undefined {
  let current: OptionNode | undefined = root
  for (const name of path) {
    current = current?.items?.find((item) => item.name === name)
    if (!current) return undefined
  }
  return current
}

describe('rewards config in the settings page', () => {
  const tree = loadTree('artalk.example.zh-CN.yml')

  it('shows the rewards group at the top level with a readable title', () => {
    const rewards = findNode(tree, ['rewards'])
    expect(rewards).toBeDefined()
    expect(rewards?.title).toBe('积分、徽章与 UID 靓号')
    expect(rewards?.level).toBe(1)
  })

  it('labels each subgroup instead of falling back to the raw key', () => {
    // 没有注释的节点会显示成 camelCase 的键名（points / badges / uid），
    // 中文界面里那种英文小写标题就是缺注释的症状。
    expect(findNode(tree, ['rewards', 'points'])?.title).toBe('积分获取规则')
    expect(findNode(tree, ['rewards', 'badges'])?.title).toBe('用户徽章')
    expect(findNode(tree, ['rewards', 'uid'])?.title).toBe('UID 靓号')
  })

  it('exposes the switches an admin actually needs', () => {
    expect(findNode(tree, ['rewards', 'enabled'])?.type).toBe('boolean')
    expect(findNode(tree, ['rewards', 'points', 'checkin'])?.type).toBe('number')
    expect(findNode(tree, ['rewards', 'badges', 'enabled'])?.type).toBe('boolean')
    expect(findNode(tree, ['rewards', 'badges', 'disable_auto_grant'])?.type).toBe('boolean')
  })

  it('turns the parenthesised hint into a subtitle', () => {
    expect(findNode(tree, ['rewards', 'badges', 'max_equipped'])?.subTitle).toContain('当前仅支持')
    expect(findNode(tree, ['rewards', 'enabled'])?.subTitle).toContain('积分、徽章、靓号')
  })

  it('addresses every price tier separately so they can be edited', () => {
    const price = findNode(tree, ['rewards', 'uid', 'price'])
    expect(price?.type).toBe('object')
    expect(price?.items?.map((item) => item.name)).toEqual(['s', 'a', 'b', 'c'])
    expect(price?.items?.[0].title).toContain('全位相同')
  })
})

describe('the settings page index', () => {
  const groups = buildRootGroups(loadTree('artalk.example.zh-CN.yml'))
  const names = groups.map((group) => group.name)

  it('lists the rewards section in the left-hand index', () => {
    // 这一条是"配置项写了却在界面上找不到"的守门员：根节点如果没有子节点，
    // 会被压平进「配置文件」；如果落在 HIDDEN_ROOT_NODES 里，会整个消失。
    expect(names).toContain('rewards')
  })

  it('puts it where the template declares it', () => {
    const index = names.indexOf('rewards')
    expect(index).toBeGreaterThan(0)
    // 模板顺序是 security → rewards → console → img_upload，前两个有独立页面
    // 被隐藏，所以 rewards 后面紧跟的就是图片上传。
    expect(names[index + 1]).toBe('img_upload')
  })

  it('keeps the sections that have their own pages out of the index', () => {
    expect(names).not.toContain('security')
    expect(names).not.toContain('console')
    expect(names).not.toContain('captcha')
    expect(names).not.toContain('admin_users')
  })

  it('still flattens the single-field options into one basic group', () => {
    expect(names[0]).toBe('__basic__')
    const basic = groups[0]
    expect(basic.items?.map((item) => item.name)).toContain('port')
    expect(basic.items?.map((item) => item.name)).not.toContain('rewards')
  })
})

describe('every locale template carries the same rewards keys', () => {
  const keys = [
    ['rewards', 'enabled'],
    ['rewards', 'points', 'comment_create'],
    ['rewards', 'points', 'comment_create_daily_cap'],
    ['rewards', 'points', 'vote_received'],
    ['rewards', 'points', 'checkin'],
    ['rewards', 'badges', 'enabled'],
    ['rewards', 'badges', 'max_equipped'],
    ['rewards', 'badges', 'disable_auto_grant'],
    ['rewards', 'uid', 'enabled'],
    ['rewards', 'uid', 'price', 's'],
    ['rewards', 'uid', 'price', 'a'],
    ['rewards', 'uid', 'price', 'b'],
    ['rewards', 'uid', 'price', 'c'],
  ]

  for (const name of [
    'artalk.example.yml',
    'artalk.example.zh-CN.yml',
    'artalk.example.zh-TW.yml',
    'artalk.example.pt-PT.yml',
    'artalk.example.tr.yml',
    'artalk.example.simple.yml',
  ]) {
    it(name, () => {
      const tree = loadTree(name)
      for (const path of keys) {
        expect(findNode(tree, path), `${name} is missing ${path.join('.')}`).toBeDefined()
      }
    })
  }
})
