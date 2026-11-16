// Keep copying/downloading independent from display/search. Raw must not pass
// through log-output normalization (trim, NFC or control-character replacement).
export function subscriptionPreviewDelivery(content: string, encoding?: string) {
  if (encoding !== 'base64') return content
  const bytes = new TextEncoder().encode(content)
  let binary = ''
  for (let offset = 0; offset < bytes.length; offset += 8192) binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
  return btoa(binary)
}

export function subscriptionPreviewExtension(contentType: string) {
  return contentType.includes('json') ? 'json' : contentType.includes('yaml') ? 'yaml' : 'txt'
}
