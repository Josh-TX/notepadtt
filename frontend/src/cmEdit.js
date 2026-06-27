// Computes the smallest single CodeMirror-style edit that turns oldStr into newStr,
// by trimming the longest common prefix/suffix and replacing only the differing middle.
// Used instead of a full setValue() so an in-flight (not-yet-polled) keystroke survives
// a remote content update.

function commonPrefixLength(a, b) {
  const max = Math.min(a.length, b.length)
  let i = 0
  while (i < max && a[i] === b[i]) i++
  return i
}

function commonSuffixLength(a, b, maxLen) {
  let i = 0
  while (i < maxLen && a[a.length - 1 - i] === b[b.length - 1 - i]) i++
  return i
}

function offsetToPos(str, offset) {
  let line = 0
  let lineStart = 0
  for (let i = 0; i < offset; i++) {
    if (str[i] === '\n') {
      line++
      lineStart = i + 1
    }
  }
  return { line, ch: offset - lineStart }
}

export function computeMinimalEdit(oldStr, newStr) {
  if (oldStr === newStr) return null

  const prefixLen = commonPrefixLength(oldStr, newStr)
  const maxSuffixLen = Math.min(oldStr.length - prefixLen, newStr.length - prefixLen)
  const suffixLen = commonSuffixLength(oldStr, newStr, maxSuffixLen)

  const from = offsetToPos(oldStr, prefixLen)
  const to = offsetToPos(oldStr, oldStr.length - suffixLen)
  const text = newStr.slice(prefixLen, newStr.length - suffixLen)

  return { from, to, text }
}
