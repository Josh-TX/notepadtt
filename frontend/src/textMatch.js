// Returns merged [start, end) ranges of case-insensitive term matches within text.
export function findMatchRanges(text, terms) {
  const ranges = []
  const lower = text.toLowerCase()
  for (const term of terms) {
    const lt = term.toLowerCase()
    if (!lt) continue
    let i = 0
    while ((i = lower.indexOf(lt, i)) !== -1) {
      ranges.push([i, i + lt.length])
      i += lt.length
    }
  }

  ranges.sort((a, b) => a[0] - b[0])
  const merged = []
  for (const [s, e] of ranges) {
    if (merged.length && s <= merged[merged.length - 1][1]) {
      merged[merged.length - 1][1] = Math.max(merged[merged.length - 1][1], e)
    } else {
      merged.push([s, e])
    }
  }
  return merged
}
