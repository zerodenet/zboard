import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(join(import.meta.dirname, '..', 'views', 'Nodes.vue'), 'utf8')

describe('new VPS onboarding policy', () => {
  it('always continues from VPS registration into SSH setup', () => {
    const createStart = source.indexOf('async function create()')
    const createEnd = source.indexOf('function openEdit', createStart)
    const createBlock = source.slice(createStart, createEnd)
    expect(createBlock).toContain('pendingBBRNodeID.value = wantsBBR ? node.id : 0')
    expect(createBlock).toContain('openSSH(selectedNode.value)')
    expect(createBlock).not.toContain("else message.value = 'VPS 已登记；协议服务可在协议页面单独创建。'")
  })

  it('verifies SSH after every saved SSH configuration and continues to Zero initialization', () => {
    expect(source).toContain('async function runNodeOnboardingAfterSSHSave(nodeID: number)')
    expect(source).toContain('await runNodeOnboardingAfterSSHSave(nodeID)')
    expect(source).toContain('const result = await testNodeSSH(nodeID)')
    expect(source).toContain("detailSection.value = 'kernel'")
  })

  it('keeps BBR optional and outside the Zero bootstrap critical path', () => {
    const onboardingStart = source.indexOf('async function runNodeOnboardingAfterSSHSave')
    const onboardingEnd = source.indexOf('async function saveSSH', onboardingStart)
    const onboarding = source.slice(onboardingStart, onboardingEnd)
    const zeroNavigation = onboarding.indexOf("detailSection.value = 'kernel'")
    const optionalBBR = onboarding.indexOf('void runOptionalBBRInitialization(nodeID)')
    expect(source).toContain('async function runOptionalBBRInitialization(nodeID: number)')
    expect(source).toContain('Zero 初始化不受影响。')
    expect(zeroNavigation).toBeGreaterThanOrEqual(0)
    expect(optionalBBR).toBeGreaterThan(zeroNavigation)
    expect(source).not.toContain('runPendingBBRInitialization')
  })
})
