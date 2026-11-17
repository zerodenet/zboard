import axios, { AxiosError, type AxiosAdapter } from 'axios'
import { afterEach, describe, expect, it } from 'vitest'
import { authenticatedAPI } from './client'
import { pluginAction, importPlugin, type ImportPreview, type Plugin, type PluginLifecycleAction } from './plugins'
import requests from './testdata/pluginLifecycleRequests.json'
import { normalizeApiErrorMessage } from '../utils/apiError'

const adapter = authenticatedAPI.defaults.adapter
afterEach(() => { authenticatedAPI.defaults.adapter = adapter })

describe('plugin lifecycle HTTP request contract', () => {
  it.each(requests)('serializes the shared handler request for $action without retired fields', async request => {
    let serialized = ''
    let path = ''
    authenticatedAPI.defaults.adapter = (async config => {
      serialized = config.data
      path = config.url || ''
      return { status: 200, statusText: 'OK', config, headers: {}, data: { data: { id: 'example.server' } } }
    }) satisfies AxiosAdapter
    await pluginAction({ id: 'example.server', generation: request.body.generation } as Plugin, request.action as PluginLifecycleAction)
    expect(path).toBe(`/admin/plugins/example.server/actions/${request.action}`)
    expect(typeof serialized).toBe('string')
    expect(JSON.parse(serialized)).toEqual(request.body)
    expect(JSON.parse(serialized)).not.toHaveProperty('version_id')
  })

  it.each([
    [409, 'plugin changed; refresh and retry', '插件状态或配置已更新'],
    [403, 'admin required', '管理员权限'],
    [403, 'plugin capability is outside the host admission scope', '宿主能力准入'],
    [400, 'plugin API or runtime is incompatible', '运行平台与当前宿主不兼容'],
    [400, 'plugin lifecycle data preparation is incomplete or incompatible', '数据准备尚未完成'],
    [400, 'plugin process failed to start', '进程启动失败'],
    [400, 'invalid or oversized plugin request', '不支持的字段'],
  ])('preserves the actionable host error through Axios for HTTP %s: %s', async (status, message, expected) => {
    authenticatedAPI.defaults.adapter = async config => {
      const response = { status: Number(status), statusText: 'Rejected', config, headers: {}, data: { message, error: { version: 1, code: 'invalid_argument' } } }
      throw new AxiosError('rejected', axios.AxiosError.ERR_BAD_REQUEST, config, undefined, response)
    }
    const cause = await pluginAction({ id: 'example.server', generation: 1 } as Plugin, 'enable').catch(error => error)
    expect(normalizeApiErrorMessage(cause, '未知失败', 'plugin-management')).toContain(expected)
  })
})

it('binds offline publisher migration to the reviewed archive and verified key without implying activation', async () => {
 const requests: Array<Record<string, string>> = []
 authenticatedAPI.defaults.adapter = (async config => {
   requests.push(config.headers.toJSON() as Record<string, string>)
   return { status: 200, statusText: 'OK', config, headers: {}, data: { data: { id: 'example.plugin' } } }
 }) satisfies AxiosAdapter
 const file = new File(['archive'], 'plugin.zbplugin')
 const preview = { digest: 'digest', public_key: 'key', fingerprint: 'fingerprint', publisher_migration: { confirmation: 'state-binding' } } as ImportPreview
 await importPlugin(file, preview, false)
 await importPlugin(file, preview, false, true)
 expect(requests[0]['X-Plugin-Publisher-Migration']).toBe('')
 expect(requests[1]['X-Plugin-Publisher-Migration']).toBe('state-binding')
 expect(requests[1]['X-Plugin-Trust-Fingerprint']).toBe('fingerprint')
 expect(requests[1]['X-Plugin-Digest']).toBe('digest')
 expect(requests[1]['X-Plugin-Public-Key']).toBe('key')
})
