import { useEffect, useMemo, useState } from 'react'
import './App.css'

interface FuncKey {
  pkg: string
  recv: string | null
  name: string
}

type Callee =
  | { Local: FuncKey }
  | { External: string }
  | { Stdlib: string }

interface CallInfo {
  callee: Callee
  goroutine: boolean
}

interface BodyElement {
  kind: 'Call' | 'Loop'
  info?: CallInfo
  body?: BodyElement[]
}

interface AnalysisPayload {
  root: FuncKey
  body: BodyElement[]
  graph: Array<[FuncKey, BodyElement[]]>
}

type ConnState = 'connecting' | 'open' | 'closed'
type CalleeKind = 'local' | 'external' | 'stdlib'

function formatFuncKey(k: FuncKey): string {
  if (k.recv) return `${k.pkg}.(${k.recv}).${k.name}`
  return `${k.pkg}.${k.name}`
}

interface CalleeParts {
  kind: CalleeKind
  pkg: string | null
  recv: string | null
  label: string
  key: FuncKey | null
}

/** Split a callee into package qualifier, receiver, identifier and kind. */
function describeCallee(callee: Callee): CalleeParts {
  if ('Local' in callee) {
    const k = callee.Local
    return { kind: 'local', pkg: k.pkg, recv: k.recv, label: k.name, key: k }
  }
  if ('External' in callee) {
    return splitQualified('external', callee.External)
  }
  return splitQualified('stdlib', callee.Stdlib)
}

function splitQualified(kind: CalleeKind, raw: string): CalleeParts {
  const idx = raw.lastIndexOf('.')
  if (idx > 0) {
    return { kind, pkg: raw.slice(0, idx), recv: null, label: raw.slice(idx + 1), key: null }
  }
  return { kind, pkg: null, recv: null, label: raw, key: null }
}

/* ------------------------------------------------------------------ */
/*  Tree node                                                          */
/* ------------------------------------------------------------------ */

function TreeNode({
  elem,
  graphMap,
  depth,
  path,
  expanded,
  toggle,
  stack,
}: {
  elem: BodyElement
  graphMap: Map<string, BodyElement[]>
  depth: number
  path: string
  expanded: Set<string>
  toggle: (path: string) => void
  stack: Set<string>
}) {
  const open = expanded.has(path)

  /* ---- Loop ---- */
  if (elem.kind === 'Loop') {
    const children = elem.body ?? []
    return (
      <div className="tree-branch">
        <div
          className={`row row-loop ${open ? 'is-open' : ''}`}
          style={{ ['--depth' as string]: depth }}
          onClick={() => toggle(path)}
        >
          <Gutter depth={depth} expandable open={open} />
          <span className="row-icon loop">⟳</span>
          <span className="row-label loop">loop</span>
          <span className="badge badge-loop">iterates</span>
          <span className="row-count">{children.length}</span>
        </div>
        {open && (
          <div className="children">
            {children.map((child, i) => (
              <TreeNode
                key={i}
                elem={child}
                graphMap={graphMap}
                depth={depth + 1}
                path={`${path}/${i}`}
                expanded={expanded}
                toggle={toggle}
                stack={stack}
              />
            ))}
          </div>
        )}
      </div>
    )
  }

  /* ---- Call ---- */
  if (elem.kind === 'Call' && elem.info) {
    const info = elem.info
    const { kind, pkg, recv, label, key } = describeCallee(info.callee)

    const keyStr = key ? JSON.stringify(key) : null
    const subBody = keyStr ? graphMap.get(keyStr) : undefined
    const isRecursive = keyStr ? stack.has(keyStr) : false
    const expandable = !!subBody && subBody.length > 0 && !isRecursive
    const nextStack = keyStr ? new Set(stack).add(keyStr) : stack

    return (
      <div className="tree-branch">
        <div
          className={`row row-call ${expandable ? 'expandable' : ''} ${
            open ? 'is-open' : ''
          }`}
          style={{ ['--depth' as string]: depth }}
          onClick={() => expandable && toggle(path)}
        >
          <Gutter depth={depth} expandable={expandable} open={open} />
          <span className={`kind-dot kind-${kind}`} />
          {info.goroutine && <span className="kw">go</span>}
          <span className="row-name">
            {pkg && <span className="qual-pkg">{pkg}.</span>}
            {recv && <span className="qual-recv">({recv}).</span>}
            <span className={`ident kind-${kind}`}>{label}</span>
          </span>
          <span className="row-tags">
            {info.goroutine && <span className="badge badge-goroutine">goroutine</span>}
            {kind === 'external' && <span className="badge badge-external">external</span>}
            {kind === 'stdlib' && <span className="badge badge-stdlib">stdlib</span>}
            {isRecursive && <span className="badge badge-recursive">recursive ↺</span>}
          </span>
          {expandable && <span className="row-count">{subBody!.length}</span>}
        </div>
        {open && expandable && (
          <div className="children">
            {subBody!.map((child, i) => (
              <TreeNode
                key={i}
                elem={child}
                graphMap={graphMap}
                depth={depth + 1}
                path={`${path}/${i}`}
                expanded={expanded}
                toggle={toggle}
                stack={nextStack}
              />
            ))}
          </div>
        )}
      </div>
    )
  }

  return null
}

/** Indent guides + expand chevron, aligned to the node's depth. */
function Gutter({
  depth,
  expandable,
  open,
}: {
  depth: number
  expandable?: boolean
  open?: boolean
}) {
  return (
    <span className="gutter">
      {Array.from({ length: depth }).map((_, i) => (
        <span key={i} className="guide" />
      ))}
      <span className={`chevron ${expandable ? 'has' : ''} ${open ? 'open' : ''}`}>
        {expandable ? '▸' : ''}
      </span>
    </span>
  )
}

/* ------------------------------------------------------------------ */
/*  Expansion helpers                                                  */
/* ------------------------------------------------------------------ */

/** Walk the tree collecting every expandable path (cycle-safe, depth-capped). */
function collectAllPaths(
  body: BodyElement[],
  graphMap: Map<string, BodyElement[]>,
): Set<string> {
  const out = new Set<string>()
  const MAX_DEPTH = 200

  const walk = (elems: BodyElement[], base: string, stack: Set<string>, depth: number) => {
    if (depth > MAX_DEPTH) return
    elems.forEach((elem, i) => {
      const path = `${base}/${i}`
      if (elem.kind === 'Loop' && elem.body?.length) {
        out.add(path)
        walk(elem.body, path, stack, depth + 1)
      } else if (elem.kind === 'Call' && elem.info) {
        const c = elem.info.callee
        if ('Local' in c) {
          const keyStr = JSON.stringify(c.Local)
          const sub = graphMap.get(keyStr)
          if (sub?.length && !stack.has(keyStr)) {
            out.add(path)
            const next = new Set(stack).add(keyStr)
            walk(sub, path, next, depth + 1)
          }
        }
      }
    })
  }

  walk(body, 'r', new Set(), 0)
  return out
}

interface Stats {
  functions: number
  calls: number
  external: number
  stdlib: number
  goroutines: number
  loops: number
}

function computeStats(data: AnalysisPayload): Stats {
  const s: Stats = { functions: data.graph.length, calls: 0, external: 0, stdlib: 0, goroutines: 0, loops: 0 }
  const scan = (elems: BodyElement[]) => {
    for (const el of elems) {
      if (el.kind === 'Loop') {
        s.loops++
        if (el.body) scan(el.body)
      } else if (el.info) {
        s.calls++
        if (el.info.goroutine) s.goroutines++
        const c = el.info.callee
        if ('External' in c) s.external++
        else if ('Stdlib' in c) s.stdlib++
      }
    }
  }
  scan(data.body)
  data.graph.forEach(([, body]) => scan(body))
  return s
}

/* ------------------------------------------------------------------ */
/*  App                                                                */
/* ------------------------------------------------------------------ */

function App() {
  const [data, setData] = useState<AnalysisPayload | null>(null)
  const [conn, setConn] = useState<ConnState>('connecting')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  useEffect(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws`)

    ws.onopen = () => setConn('open')
    ws.onmessage = (event) => {
      try {
        const payload: AnalysisPayload = JSON.parse(event.data)
        setData(payload)
        // Start fully folded; the user expands from the root outward.
        setExpanded(new Set())
      } catch (e) {
        console.error(e)
      }
    }
    ws.onclose = () => setConn('closed')
    ws.onerror = () => setConn('closed')
    return () => ws.close()
  }, [])

  const graphMap = useMemo(() => {
    const m = new Map<string, BodyElement[]>()
    data?.graph.forEach(([key, body]) => m.set(JSON.stringify(key), body))
    return m
  }, [data])

  const stats = useMemo(() => (data ? computeStats(data) : null), [data])

  const toggle = (path: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })

  const expandAll = () => {
    if (data) setExpanded(collectAllPaths(data.body, graphMap))
  }
  const collapseAll = () => setExpanded(new Set())

  const connMeta: Record<ConnState, { text: string; cls: string }> = {
    connecting: { text: 'Connecting', cls: 'connecting' },
    open: { text: 'Live', cls: 'open' },
    closed: { text: 'Disconnected', cls: 'closed' },
  }

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark" />
          <span className="brand-name">Call Analyzer</span>
          <span className="brand-sub">Go static call tracer</span>
        </div>

        {data && (
          <div className="root-chip" title="Entry point">
            <span className="root-chip-label">entry</span>
            <span className="root-chip-fn">{formatFuncKey(data.root)}</span>
          </div>
        )}

        <div className="topbar-right">
          <div className={`conn conn-${connMeta[conn].cls}`}>
            <span className="conn-dot" />
            {connMeta[conn].text}
          </div>
        </div>
      </header>

      {data && stats && (
        <div className="subbar">
          <div className="controls">
            <button className="btn" onClick={expandAll}>Expand all</button>
            <button className="btn" onClick={collapseAll}>Collapse all</button>
          </div>

          <div className="stats">
            <Stat value={stats.functions} label="functions" />
            <Stat value={stats.calls} label="calls" />
            <Stat value={stats.external} label="external" tone="external" />
            <Stat value={stats.stdlib} label="stdlib" tone="stdlib" />
            <Stat value={stats.goroutines} label="goroutines" tone="goroutine" />
            <Stat value={stats.loops} label="loops" tone="loop" />
          </div>

          <div className="legend">
            <LegendItem tone="local" label="local" />
            <LegendItem tone="external" label="external" />
            <LegendItem tone="stdlib" label="stdlib" />
          </div>
        </div>
      )}

      <main className="viewport">
        {data ? (
          <div className="tree">
            <div className="tree-root">
              <span className="kind-dot kind-local" />
              <span className="tree-root-fn">{formatFuncKey(data.root)}</span>
              <span className="tree-root-tag">root</span>
            </div>
            {data.body.length === 0 && (
              <div className="tree-empty">This function makes no tracked calls.</div>
            )}
            {data.body.map((elem, idx) => (
              <TreeNode
                key={idx}
                elem={elem}
                graphMap={graphMap}
                depth={0}
                path={`r/${idx}`}
                expanded={expanded}
                toggle={toggle}
                stack={new Set()}
              />
            ))}
          </div>
        ) : (
          <div className="empty-state">
            <div className="empty-icon">⌘</div>
            <div className="empty-title">
              {conn === 'closed' ? 'Connection lost' : 'Waiting for analysis'}
            </div>
            <div className="empty-desc">
              Trigger a trace by sending a <code>file:line</code> over TCP:
            </div>
            <pre className="empty-cmd">echo "path/to/file.go:42" | nc 127.0.0.1 2222</pre>
          </div>
        )}
      </main>
    </div>
  )
}

function Stat({ value, label, tone }: { value: number; label: string; tone?: string }) {
  return (
    <div className="stat">
      <span className={`stat-value ${tone ? `tone-${tone}` : ''}`}>{value}</span>
      <span className="stat-label">{label}</span>
    </div>
  )
}

function LegendItem({ tone, label }: { tone: string; label: string }) {
  return (
    <span className="legend-item">
      <span className={`kind-dot kind-${tone}`} />
      {label}
    </span>
  )
}

export default App
