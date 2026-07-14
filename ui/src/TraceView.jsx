import { useMemo, useState } from 'react'
import { downloadMarkdown } from './exportMd.js'

// Badge text/colors per node kind. Purely presentational — the kinds are
// decided by the backend.
const BADGES = {
  go: { text: 'go', cls: 'b-go' },
  loop: { text: 'loop', cls: 'b-loop' },
  'chan-send': { text: 'send', cls: 'b-send' },
  'chan-recv': { text: 'recv', cls: 'b-recv' },
  'chan-close': { text: 'close', cls: 'b-close' },
  select: { text: 'select', cls: 'b-select' },
  'interface-call': { text: 'iface', cls: 'b-iface' },
  'func-value-call': { text: 'fn val', cls: 'b-fnval' },
  'indirect-call': { text: 'dyn', cls: 'b-fnval' },
  impl: { text: 'impl', cls: 'b-impl' },
  bound: { text: 'bound', cls: 'b-impl' },
  defer: { text: 'defer', cls: 'b-defer' },
  return: { text: 'return', cls: 'b-return' },
  arg: { text: 'arg', cls: 'b-arg' },
  param: { text: 'param', cls: 'b-arg' },
  race: { text: 'race', cls: 'b-race' },
  'race-warn': { text: 'race warn', cls: 'b-warn' },
  'chan-closed': { text: 'closed ch', cls: 'b-close' },
  'chan-closed-warn': { text: 'closed ch warn', cls: 'b-warn' },
  'fd-leak': { text: 'fd leak', cls: 'b-fd' },
  'fd-leak-warn': { text: 'fd leak warn', cls: 'b-warn' },
  'go-leak': { text: 'leak', cls: 'b-leak' },
  'go-leak-warn': { text: 'leak warn', cls: 'b-warn' },
  peer: { text: 'peer', cls: 'b-peer' },
  note: { text: 'ℹ', cls: 'b-note' },
}

const VAR_PALETTE_SIZE = 10

// Kinds rendered as a leading code keyword rather than a badge.
const KW = { go: 'go', defer: 'defer', select: 'select', return: 'return' }

// Kinds whose meaning is already carried by the row text (a keyword, an arrow,
// or the "←" of an annotation), so a badge would just add noise.
const HIDE_BADGE = new Set([
  'go', 'defer', 'select', 'return',
  'chan-send', 'chan-recv', 'chan-close',
  'arg', 'param', 'peer',
])

function walk(n, fn) {
  fn(n)
  n.kids?.forEach((k) => walk(k, fn))
}

function countSubtree(n) {
  let c = 1
  n.kids?.forEach((k) => (c += countSubtree(k)))
  return c
}

function nodeHasVar(n, v) {
  return n.spans?.some((s) => s.v === v) ?? false
}

// matchTree marks a node visible if it, or any descendant, contains the
// query in its text/pos/kind/label (plain string matching for display
// filtering only).
function matchTree(n, q, out) {
  const self =
    n.text?.toLowerCase().includes(q) ||
    n.pos?.toLowerCase().includes(q) ||
    n.kind?.toLowerCase().includes(q) ||
    n.label?.toLowerCase().includes(q)
  let child = false
  n.kids?.forEach((k) => {
    child = matchTree(k, q, out) || child
  })
  if (self || child) out.add(n._id)
  return self || child
}

export default function TraceView({ trace }) {
  const [collapsed, setCollapsed] = useState(() => new Set())
  const [filter, setFilter] = useState('')
  // tracked is the variable being followed: {v, name}
  const [tracked, setTracked] = useState(null)

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return null
    const out = new Set()
    matchTree(trace.root, q, out)
    return out
  }, [filter, trace.root])

  const trackedCount = useMemo(() => {
    if (!tracked) return 0
    let c = 0
    walk(trace.root, (n) => {
      c += n.spans?.filter((s) => s.v === tracked.v).length ?? 0
    })
    return c
  }, [tracked, trace.root])

  const toggle = (id) =>
    setCollapsed((prev) => {
      const s = new Set(prev)
      if (s.has(id)) s.delete(id)
      else s.add(id)
      return s
    })

  const collapseAll = () => {
    const s = new Set()
    walk(trace.root, (n) => {
      if (n !== trace.root && n.kids?.length) s.add(n._id)
    })
    setCollapsed(s)
    setTracked(null)
  }

  // Clicking a variable tracks it: highlight every occurrence and expand
  // every collapsed path that contains one.
  const clickVar = (v, name) => {
    if (tracked?.v === v) {
      setTracked(null)
      return
    }
    setTracked({ v, name })
    const onPath = new Set()
    const mark = (n) => {
      let has = nodeHasVar(n, v)
      n.kids?.forEach((k) => {
        if (mark(k)) has = true
      })
      if (has) onPath.add(n._id)
      return has
    }
    mark(trace.root)
    setCollapsed((prev) => new Set([...prev].filter((id) => !onPath.has(id))))
  }

  return (
    <div className="trace-view">
      <div className="trace-header">
        <h2 className="trace-target">{trace.target}</h2>
        {trace.params &&
          Object.entries(trace.params).map(([k, v]) => (
            <span key={k} className="chip">
              {k}={v}
            </span>
          ))}
        <span className="trace-nodes">{trace.nodeCount} nodes</span>
        <span className="trace-time">{trace.time}</span>
      </div>

      <div className="toolbar">
        <input
          className="filter"
          placeholder="filter rows… (text, pos, kind, label)"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {filter && (
          <button className="ghost" onClick={() => setFilter('')}>
            ✕
          </button>
        )}
        {tracked && (
          <button className="ghost tracking" onClick={() => setTracked(null)}>
            tracking{' '}
            <span className={`var v${tracked.v % VAR_PALETTE_SIZE} var-sel`}>{tracked.name}</span>{' '}
            · {trackedCount}× · ✕
          </button>
        )}
        <span className="toolbar-gap" />
        {trace.type === 'scan' && (
          <button className="ghost" onClick={() => downloadMarkdown(trace)} title="download this scan as a markdown report">
            export .md
          </button>
        )}
        <button className="ghost" onClick={() => setCollapsed(new Set())}>
          expand all
        </button>
        <button className="ghost" onClick={collapseAll}>
          collapse all
        </button>
      </div>

      <div className="tree">
        <Row
          n={trace.root}
          depth={0}
          collapsed={collapsed}
          onToggle={toggle}
          visible={visible}
          tracked={tracked}
          onVarClick={clickVar}
        />
      </div>

      <div className="legend">
        <span className="chip label-stdlib">stdlib</span> /{' '}
        <span className="chip label-module">module</span> not traced into ·{' '}
        <span className="kw kw-go">go</span>
        <span className="kw kw-defer">defer</span>
        <span className="kw kw-return">return</span> keywords ·{' '}
        <span className="var v3 legend-var">variable</span> click to track ·{' '}
        <span className="var v0 legend-var var-ret">value</span> returned
      </div>
    </div>
  )
}

function VarSpan({ s, tracked, onVarClick }) {
  return (
    <button
      className={`var v${s.v % VAR_PALETTE_SIZE} ${tracked?.v === s.v ? 'var-sel' : ''} ${s.r ? 'var-ret' : ''}`}
      title={s.r ? 'returned value · click to track' : 'click to track this variable'}
      onClick={(e) => {
        e.stopPropagation()
        onVarClick(s.v, s.t)
      }}
    >
      {s.t}
    </button>
  )
}

function NodeText({ n, tracked, onVarClick }) {
  const kw = KW[n.kind]
  const keyword = kw ? <span className={`kw kw-${n.kind}`}>{kw}</span> : null

  // Plain-text node (no variable spans), e.g. defer/select/note.
  if (!n.spans) {
    const rest = kw && n.text.trim() === kw ? '' : n.text
    return (
      <span className="text">
        {keyword}
        {rest}
      </span>
    )
  }

  // Strip a leading keyword already present in the text so it isn't shown
  // twice (the colored keyword token replaces it).
  let spans = n.spans
  if (kw && spans.length && !spans[0].v) {
    const trimmed = spans[0].t.replace(/^\s+/, '')
    if (trimmed.startsWith(kw)) {
      spans = [{ ...spans[0], t: trimmed.slice(kw.length).replace(/^\s+/, '') }, ...spans.slice(1)]
    }
  }

  return (
    <span className="text">
      {keyword}
      {spans.map((s, i) =>
        s.v ? (
          <VarSpan key={i} s={s} tracked={tracked} onVarClick={onVarClick} />
        ) : (
          <span key={i}>{s.t}</span>
        ),
      )}
    </span>
  )
}

function Row({ n, depth, collapsed, onToggle, visible, tracked, onVarClick }) {
  if (visible && !visible.has(n._id)) return null
  const kids = n.kids ?? []
  const isCollapsed = !visible && collapsed.has(n._id)
  const badge = BADGES[n.kind]
  const showBadge = badge && !HIDE_BADGE.has(n.kind)
  const badgeText = n.kind === 'loop' && n.num ? `loop ${n.num}` : badge?.text
  const hit = tracked && nodeHasVar(n, tracked.v)

  return (
    <>
      <div className={`row k-${n.kind || 'plain'} ${hit ? 'row-hit' : tracked ? 'row-dim' : ''}`}>
        <span className="gutter" title={n.pos}>
          {n.pos}
        </span>
        <span className="row-body" style={{ '--depth': depth }}>
          {kids.length > 0 ? (
            <button className="disc" onClick={() => onToggle(n._id)}>
              {isCollapsed ? '▸' : '▾'}
            </button>
          ) : (
            <span className="disc spacer" />
          )}
          {showBadge && <span className={`badge ${badge.cls}`}>{badgeText}</span>}
          <NodeText n={n} tracked={tracked} onVarClick={onVarClick} />
          {n.label && <span className={`chip label-${n.label}`}>{n.label}</span>}
          {isCollapsed && <span className="hidden-count">+{countSubtree(n) - 1} hidden</span>}
        </span>
      </div>
      {!isCollapsed &&
        kids.map((k) => (
          <Row
            key={k._id}
            n={k}
            depth={depth + 1}
            collapsed={collapsed}
            onToggle={onToggle}
            visible={visible}
            tracked={tracked}
            onVarClick={onVarClick}
          />
        ))}
    </>
  )
}
