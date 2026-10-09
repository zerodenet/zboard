import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, createTicket, replyTicket } from './client'
import { uploadFile } from './files'

describe('file transport and ticket attachment contracts', () => {
  beforeEach(() => vi.restoreAllMocks())
  it('sends multipart data and lets Axios set the boundary', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { data: { id: 'file-id' } } })
    const file = new File(['trace'], 'trace.log')
    await uploadFile(file, 'ticket')
    const [path, data, options] = post.mock.calls[0]
    expect(path).toBe('/files')
    expect((data as FormData).get('file')).toBe(file)
    expect((data as FormData).get('purpose')).toBe('ticket')
    expect(options?.headers).toBeUndefined()
  })
  it('submits file identities and URL references in both create and reply requests', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { data: {} } })
    const attachments = [{ file_id: 'private-file', name: 'log', url: '/api/v1/files/private-file' }, { name: 'remote', url: 'https://files.example/a' }]
    const expected = [{ file_id: 'private-file' }, { name: 'remote', url: 'https://files.example/a' }]
    await createTicket({ subject: 'subject', category: 'other', priority: 1, body: 'details', attachments })
    expect(post.mock.calls[0][1]).toEqual(expect.objectContaining({ attachments: expected }))
    await replyTicket(1, 'reply', true, attachments)
    expect(post.mock.calls[1]).toEqual(['/admin/tickets/1/messages', { body: 'reply', attachments: expected }])
  })
})
