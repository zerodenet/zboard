import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const protocolsView = readFileSync(new URL('../views/Protocols.vue', import.meta.url), 'utf8')

describe('protocol editor save flow', () => {
  it('keeps the review step when earlier wizard steps validate successfully', () => {
    const validateStart = protocolsView.indexOf('async function validateStep(step: number)')
    const validateEnd = protocolsView.indexOf('async function nextStep()', validateStart)
    const validateStep = protocolsView.slice(validateStart, validateEnd)

    expect(validateStep).toContain('if (Object.keys(fields).length) editorStep.value = step')
    expect(validateStep).not.toContain('\n  editorStep.value = step\n')
  })

  it('still moves to the invalid step before form validation focuses a field', () => {
    const stepAssignment = protocolsView.indexOf('if (Object.keys(fields).length) editorStep.value = step')
    const validation = protocolsView.indexOf("return editorErrors.applyValidation(fields, protocolFormElement, '请修正标出的字段后继续。')")

    expect(stepAssignment).toBeGreaterThan(-1)
    expect(validation).toBeGreaterThan(stepAssignment)
  })
})
