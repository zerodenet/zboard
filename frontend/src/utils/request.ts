export function createRequestGuard() {
  let generation = 0
  return {
    begin() { return ++generation },
    invalidate() { generation += 1 },
    isCurrent(requestGeneration: number) { return requestGeneration === generation },
  }
}
