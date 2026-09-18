import { beforeEach, expect, it, vi } from 'vitest'
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('./client', () => ({ authenticatedAPI: http }))
import { fetchMarketDetail, previewMarketPlugin, confirmMarketPlugin, type ImportPreview } from './plugins'
beforeEach(() => { vi.clearAllMocks(); http.get.mockResolvedValue({ data: { data: {} } }); http.post.mockResolvedValue({ data: { data: {} } }) })
it('carries the package ID and exact selected version through detail, inspection and installation', async () => {
 const id = 'org.zerodenet.connect.zboard', version = '0.0.1-dev.202609150807', signal = new AbortController().signal
 await fetchMarketDetail(id, version, signal)
 expect(http.get).toHaveBeenCalledWith(`/admin/plugin-market/${id}`, expect.objectContaining({ params: { version }, signal }))
 await previewMarketPlugin(id, version, signal)
 expect(http.post).toHaveBeenLastCalledWith(`/admin/plugin-market/${id}/inspect`, undefined, expect.objectContaining({ params: { version }, signal }))
 await confirmMarketPlugin(id, version, { digest: 'digest', fingerprint: 'fingerprint' } as ImportPreview, false)
 expect(http.post).toHaveBeenLastCalledWith(`/admin/plugin-market/${id}/install`, { version, digest: 'digest', fingerprint: '' }, expect.any(Object))
})
