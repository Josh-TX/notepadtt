import CodeMirror from 'codemirror'
import 'codemirror/mode/javascript/javascript'
import 'codemirror/mode/python/python'
import 'codemirror/mode/markdown/markdown'
import 'codemirror/mode/yaml/yaml'
import 'codemirror/mode/xml/xml'
import 'codemirror/mode/htmlmixed/htmlmixed'
import 'codemirror/mode/css/css'
import 'codemirror/mode/shell/shell'
import 'codemirror/mode/sql/sql'
import 'codemirror/mode/meta'

const SUPPORTED_MODES = new Set(['javascript', 'python', 'markdown', 'yaml', 'htmlmixed', 'css', 'shell', 'sql'])

// Returns { mime, name } for the language to apply to the given filename, or null for plain
// text. markdownMode (0|1|2) controls when markdown applies to files with no recognized
// extension: 0=files named "new *", 1=files with no dot in the name, 2=only .md files.
export function getModeInfo(filename, markdownMode = 0) {
  if (!filename) return null
  const bare = filename.split('/').pop()
  const info = CodeMirror.findModeByFileName(bare)
  if (info && SUPPORTED_MODES.has(info.mode)) {
    const mime = info.mime ?? (info.mimes && info.mimes[0]) ?? null
    return { mime, name: info.name }
  }
  if (markdownMode === 0 && bare.toLowerCase().startsWith('new ')) {
    return { mime: 'text/x-markdown', name: 'Markdown' }
  }
  if (markdownMode === 1 && !bare.includes('.')) {
    return { mime: 'text/x-markdown', name: 'Markdown' }
  }
  return null
}
