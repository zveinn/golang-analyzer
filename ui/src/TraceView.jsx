import { useMemo, useState } from 'react'

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
  arg: { text: 'arg', cls: 'b-arg' },
  peer: { text: 'peer', cls: 'b-peer' },
  note: { text: 'ℹ', cls: 'b-note' },
}

function walk(n, fn) {
  fn(n)
  n.kids?.forEach((k) => walk(k, fn))
}

function countSubtree(n) {
  let c = 1
  n.kids?.forEach((k) => (c += countSubtree(k)))
  return c
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

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return null
    const out = new Set()
    matchTree(trace.root, q, out)
    return out
  }, [filter, trace.root])

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
        <span className="toolbar-gap" />
        <button className="ghost" onClick={() => setCollapsed(new Set())}>
          expand all
        </button>
        <button className="ghost" onClick={collapseAll}>
          collapse all
        </button>
      </div>

      <div className="tree">
        <Row n={trace.root} depth={0} collapsed={collapsed} onToggle={toggle} visible={visible} />
      </div>

      <div className="legend">
        <span className="chip label-local">local</span> traced into ·{' '}
        <span className="chip label-stdlib">stdlib</span> /{' '}
        <span className="chip label-module">module</span> labeled only ·{' '}
        <span className="badge b-go">go</span> goroutine launch ·{' '}
        <span className="badge b-send">send</span>
        <span className="badge b-recv">recv</span> channel ops with peers underneath
      </div>
    </div>
  )
}

function Row({ n, depth, collapsed, onToggle, visible }) {
  if (visible && !visible.has(n._id)) return null
  const kids = n.kids ?? []
  const isCollapsed = !visible && collapsed.has(n._id)
  const badge = BADGES[n.kind]
  const badgeText = n.kind === 'loop' && n.num ? `loop ${n.num}` : badge?.text

  return (
    <>
      <div className={`row k-${n.kind || 'plain'}`}>
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
          {badge && <span className={`badge ${badge.cls}`}>{badgeText}</span>}
          <span className="text">{n.text}</span>
          {n.label && <span className={`chip label-${n.label}`}>{n.label}</span>}
          {isCollapsed && <span className="hidden-count">+{countSubtree(n) - 1} hidden</span>}
        </span>
      </div>
      {!isCollapsed &&
        kids.map((k) => (
          <Row key={k._id} n={k} depth={depth + 1} collapsed={collapsed} onToggle={onToggle} visible={visible} />
        ))}
    </>
  )
}
