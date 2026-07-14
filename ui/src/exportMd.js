// Serializes a scan envelope (the structured node tree sent by the
// backend) into a markdown report. Pure formatting — the tree is rendered
// as-is, exactly like the DOM view, just as markdown.

function nodeText(n, wrapVars) {
  if (!n.spans) return n.text ?? ''
  return n.spans.map((s) => (s.v && wrapVars ? '`' + s.t + '`' : s.t)).join('')
}

// Severity labels per finding kind, mirroring the UI badges.
const KIND_LABELS = {
  race: 'RACE',
  'race-warn': 'RACE WARN',
  'chan-closed': 'CLOSED CHANNEL',
  'chan-closed-warn': 'CLOSED CHANNEL WARN',
  'fd-leak': 'FD LEAK',
  'fd-leak-warn': 'FD LEAK WARN',
  'go-leak': 'LEAK',
  'go-leak-warn': 'LEAK WARN',
}

function evidence(lines, n, depth) {
  const pad = '   '.repeat(depth)
  const pos = n.pos ? ' (`' + n.pos + '`)' : ''
  lines.push(`${pad}- ${nodeText(n, true)}${pos}`)
  n.kids?.forEach((k) => evidence(lines, k, depth + 1))
}

export function scanToMarkdown(msg) {
  const lines = []
  lines.push(`# Scan report — \`${msg.target}\``)
  lines.push('')
  lines.push(`- ${nodeText(msg.root, false)}`)
  if (msg.time) lines.push(`- Generated at: ${msg.time}`)
  lines.push(`- Tool: code-analyzer`)

  for (const cat of msg.root.kids ?? []) {
    lines.push('', `## ${nodeText(cat, false)}`)
    let i = 0
    for (const f of cat.kids ?? []) {
      if (f.kind === 'note') {
        lines.push('', `_${nodeText(f, false)}_`)
        continue
      }
      i++
      const label = KIND_LABELS[f.kind] ? `**${KIND_LABELS[f.kind]}** ` : ''
      lines.push('', `${i}. ${label}**\`${f.pos ?? '?'}\`** — ${nodeText(f, true)}`)
      f.kids?.forEach((k) => evidence(lines, k, 1))
    }
  }
  return lines.join('\n') + '\n'
}

export function scanFilename(msg) {
  const target = msg.target ?? 'scan'
  const base = target.replace(/\/+$/, '').split('/').pop() || 'scan'
  const time = (msg.time ?? '').replaceAll(':', '-')
  return `scan-${base}${time ? '-' + time : ''}.md`
}

export function downloadMarkdown(msg) {
  const blob = new Blob([scanToMarkdown(msg)], { type: 'text/markdown' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = scanFilename(msg)
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
