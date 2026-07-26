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

// Regex-mode counterpart to findMatchRanges: returns merged [start, end) ranges of
// every match of pattern within text. An invalid pattern (or one the backend accepted
// under Go's RE2 syntax but JS's regex engine rejects) yields no highlighting rather
// than throwing.
export function findRegexMatchRanges(text, pattern) {
  let re
  try {
    re = new RegExp(pattern, 'g')
  } catch {
    return []
  }
  const ranges = []
  let m
  while ((m = re.exec(text)) !== null) {
    if (m[0].length === 0) {
      re.lastIndex++
      continue
    }
    ranges.push([m.index, m.index + m[0].length])
  }
  return ranges
}
