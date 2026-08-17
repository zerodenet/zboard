import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(join(import.meta.dirname, '..', 'views', 'Nodes.vue'), 'utf8')

describe('node connector bootstrap UX', () => {
  it('explains automatic connector credential creation and rollback', () => {
    expect(source).toContain('首次 Zero 安装会自动生成并激活，无需提前手工创建')
    expect(source).toContain('本次任务会在 Zero 启动前自动生成并激活')
    expect(source).toContain("preparing_connector_credential: '准备连接凭证…'")
    expect(source).toContain("selectedNode.node_credential_prefix ? '轮换' : '手动生成'")
  })
})
