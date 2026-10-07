export function compareKernelVersions(left: string, right: string) {
  const parse = (value: string) => {
    const [core, suffix = ''] = value.trim().replace(/^v/, '').split('-', 2)
    return { numbers: core.split('.').map(item => Number(item) || 0), suffix }
  }
  const a = parse(left)
  const b = parse(right)
  for (let index = 0; index < Math.max(a.numbers.length, b.numbers.length); index += 1) {
    const difference = (a.numbers[index] || 0) - (b.numbers[index] || 0)
    if (difference) return difference > 0 ? 1 : -1
  }
  if (a.suffix === b.suffix) return 0
  if (!a.suffix) return 1
  if (!b.suffix) return -1
  const leftIdentifiers = a.suffix.split('.')
  const rightIdentifiers = b.suffix.split('.')
  for (let index = 0; index < Math.min(leftIdentifiers.length, rightIdentifiers.length); index += 1) {
    if (leftIdentifiers[index] === rightIdentifiers[index]) continue
    const leftNumber = /^\d+$/.test(leftIdentifiers[index]) ? Number(leftIdentifiers[index]) : null
    const rightNumber = /^\d+$/.test(rightIdentifiers[index]) ? Number(rightIdentifiers[index]) : null
    if (leftNumber !== null && rightNumber !== null) return leftNumber > rightNumber ? 1 : -1
    if (leftNumber !== null) return -1
    if (rightNumber !== null) return 1
    return leftIdentifiers[index].localeCompare(rightIdentifiers[index])
  }
  return leftIdentifiers.length === rightIdentifiers.length ? 0 : leftIdentifiers.length > rightIdentifiers.length ? 1 : -1
}
