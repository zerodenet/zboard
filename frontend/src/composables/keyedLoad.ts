/** Refresh only reads whose actual server parameters changed. */
export function keyedLoad(
  key: () => string,
  resource: { load: () => Promise<boolean>; reset: () => void },
) {
  let previousKey: string | undefined
  return (force = false): Promise<boolean> => {
    const nextKey = key()
    if (nextKey === previousKey && !force) return Promise.resolve(false)
    // Never label an old filter's rows or metrics with the new filter.
    if (nextKey !== previousKey) resource.reset()
    previousKey = nextKey
    return resource.load()
  }
}
