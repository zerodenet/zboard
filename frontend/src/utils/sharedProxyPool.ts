// Rewrite graph references only; protocol identity and credentials stay opaque.
export function buildSharedProxyPool(paths: Record<string, any>[], url: string, interval: number, tolerance: number) {
 if (!paths.length) throw new Error('请至少添加一个代理节点。')
 if (!Number.isInteger(interval) || interval < 1 || !Number.isInteger(tolerance) || tolerance < 0) throw new Error('测速间隔应为正整数，切换容差应为非负整数。')
 if (url && !/^https?:\/\//i.test(url)) throw new Error('测速地址必须是 HTTP 或 HTTPS URL。')
 const outbounds: Record<string,any>[] = [], groups: Record<string,any>[] = [], targets: string[] = []
 paths.forEach((source,index) => {
  const path = JSON.parse(JSON.stringify(source))
  const names = new Map<string,string>()
  for (const item of [...path.outbounds,...(path.outbound_groups || [])]) {
   if (!item.tag || names.has(item.tag)) throw new Error('代理配置的 tag 必须非空且唯一。')
   names.set(item.tag,`member-${index+1}/${item.tag}`)
  }
  const reference = (tag:string) => { const value=names.get(tag); if (!value) throw new Error(`代理配置引用不存在的 tag：${tag}`);return value }
  for (const item of path.outbounds) outbounds.push({...item,tag:reference(item.tag)})
  for (const group of path.outbound_groups || []) {
   const key = group.type === 'relay' ? 'proxies' : 'outbounds'
   groups.push({...group,tag:reference(group.tag),[key]:group[key].map(reference),...(group.selected ? {selected:reference(group.selected)} : {}),...(group.default ? {default:reference(group.default)} : {})})
  }
  targets.push(reference(path.target))
 })
 groups.push({tag:'shared',type:'url_test',outbounds:targets,...(url ? {url} : {}),interval_seconds:interval,tolerance_ms:tolerance})
 return {outbounds,outbound_groups:groups,target:'shared'}
}
