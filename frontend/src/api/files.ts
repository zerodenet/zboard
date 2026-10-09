import { api } from './client'

export interface UploadedFile {
  id: string
  name: string
  url: string
  size: number
  purpose: 'site' | 'ticket'
  content_type: string
}
export interface TicketAttachment {
  id?: number
  file_id?: string
  name: string
  url?: string
  size?: number
  content_type?: string
}

export async function uploadFile(file: File, purpose: 'site' | 'ticket', progress?: (percent: number) => void): Promise<UploadedFile> {
  const data = new FormData()
  data.append('file', file)
  data.append('purpose', purpose)
  const response = await api.post('/files', data, {
    timeout: 60_000,
    onUploadProgress: event => { if (event.total) progress?.(Math.round(event.loaded * 100 / event.total)) },
  })
  return response.data.data
}

export async function deleteFile(id: string) { await api.delete(`/files/${encodeURIComponent(id)}`) }

export async function downloadFile(id: string, name: string) {
  const response = await api.get(`/files/${encodeURIComponent(id)}`, { responseType: 'blob' })
  const url = URL.createObjectURL(response.data)
  const link = document.createElement('a')
  link.href = url; link.download = name
  document.body.append(link); link.click(); link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 30_000)
}
