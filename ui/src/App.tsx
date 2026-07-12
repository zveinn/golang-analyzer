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
  args?: string[]
}

interface BodyElement {
  kind: 'Call' | 'Loop' | 'Def'
  info?: CallInfo
  body?: BodyElement[]
  name?: string
  rhs?: string
}

interface AnalysisPayload {
  root: FuncKey
  body: BodyElement[]
  graph: Array<[FuncKey, BodyElement[]]>
  params?: Array<[FuncKey, string[]]>
  root_params?: string[]
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

/** Extract a bare identifier from an expression text (for tracing). */
function getBareId(expr: string | undefined): string | null {
  if (!expr) return null
  const t = expr.trim()
  if (/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(t)) return t
  return null
}

/** Simple check if an expression text mentions any of the currently traced names. */
function exprMentionsTraced(expr: string | undefined, traced: string[]): boolean {
  if (!expr || traced.length === 0) return false
  const t = expr.trim()
  return traced.some((nm) => nm && (t === nm || (nm.length > 1 && t.includes(nm))))
}

/* ------------------------------------------------------------------ */
/*  BodySequence: renders a list of BodyElements while simulating     */
/*  traced variable flow for the current frame.                       */
/* ------------------------------------------------------------------ */

function BodySequence({
  elems,
  graphMap,
  paramsMap,
  depth,
  basePath,
  expanded,
  toggle,
  stack,
  initialTraced,
}: {
  elems: BodyElement[]
  graphMap: Map<string, BodyElement[]>
  paramsMap: Map<string, string[]>
  depth: number
  basePath: string
  expanded: Set<string>
  toggle: (path: string) => void
  stack: Set<string>
  initialTraced: string[]
}) {
  const nodes: React.ReactNode[] = []
  const live = new Set<string>(initialTraced || [])

  elems.forEach((elem, i) => {
    const path = `${basePath}/${i}`

    if (elem.kind === 'Def') {
      // Propagate trace: if the rhs mentions a live traced name, the lhs becomes traced in this frame.
      const rhs = elem.rhs || ''
      if (elem.name && exprMentionsTraced(rhs, Array.from(live))) {
        live.add(elem.name)
      }
      // Defs are not rendered visibly (they exist only to drive tracing).
      return
    }

    if (elem.kind === 'Loop') {
      nodes.push(
        <TreeNode
          key={i}
          elem={elem}
          graphMap={graphMap}
          paramsMap={paramsMap}
          depth={depth}
          path={path}
          expanded={expanded}
          toggle={toggle}
          stack={stack}
          traced={Array.from(live)}
        />
      )
      return
    }

    if (elem.kind === 'Call' && elem.info) {
      // Which current live names flow into this call?
      const argList = elem.info.args || []
      const carriesHere = Array.from(live).filter((nm) =>
        argList.some((a) => {
          const bid = getBareId(a)
          return bid === nm || exprMentionsTraced(a, [nm])
        })
      )

      nodes.push(
        <TreeNode
          key={i}
          elem={elem}
          graphMap={graphMap}
          paramsMap={paramsMap}
          depth={depth}
          path={path}
          expanded={expanded}
          toggle={toggle}
          stack={stack}
          traced={Array.from(live)}
          carries={carriesHere}
        />
      )
      return
    }
  })

  return <>{nodes}</>
}

/* ------------------------------------------------------------------ */
/*  Tree node                                                          */
/* ------------------------------------------------------------------ */

function TreeNode({
  elem,
  graphMap,
  paramsMap,
  depth,
  path,
  expanded,
  toggle,
  stack,
  traced,
  carries,
}: {
  elem: BodyElement
  graphMap: Map<string, BodyElement[]>
  paramsMap: Map<string, string[]>
  depth: number
  path: string
  expanded: Set<string>
  toggle: (path: string) => void
  stack: Set<string>
  traced?: string[]
  carries?: string[]
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
            <BodySequence
              elems={children}
              graphMap={graphMap}
              paramsMap={paramsMap}
              depth={depth + 1}
              basePath={path}
              expanded={expanded}
              toggle={toggle}
              stack={stack}
              initialTraced={traced || []}
            />
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

    // Compute which of our current traced names flow into this call's args
    const argList = info.args || []
    const localCarries = carries || []
    // If parent passed carries, or compute from traced prop
    const effectiveCarries = localCarries.length
      ? localCarries
      : (traced || []).filter((t) =>
          argList.some((a) => {
            const b = (a || '').trim()
            return b === t || (t.length > 1 && b.includes(t))
          })
        )

    const showTracedBadge = kind !== 'stdlib' && effectiveCarries.length > 0

    // When expanding into sub-function, map the traced values by arg position to child's formal params
    let childInitialTraced: string[] | undefined
    if (keyStr && subBody) {
      const childFormals = paramsMap.get(keyStr) || []
      const mapped: string[] = []
      for (let i = 0; i < argList.length; i++) {
        const bid = getBareId(argList[i])
        if (bid && (traced || []).includes(bid) && childFormals[i]) {
          mapped.push(childFormals[i])
        }
      }
      if (mapped.length > 0) childInitialTraced = mapped
    }

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
            {showTracedBadge && (
              <span className="badge badge-traced" title={`Traced from: ${effectiveCarries.join(', ')}`}>
                traced:{effectiveCarries.join(',')}
              </span>
            )}
            {isRecursive && <span className="badge badge-recursive">recursive ↺</span>}
          </span>
          {expandable && <span className="row-count">{subBody!.length}</span>}
        </div>
        {open && expandable && (
          <div className="children">
            <BodySequence
              elems={subBody!}
              graphMap={graphMap}
              paramsMap={paramsMap}
              depth={depth + 1}
              basePath={path}
              expanded={expanded}
              toggle={toggle}
              stack={nextStack}
              initialTraced={childInitialTraced || []}
            />
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

// Also update stats scan to ignore Defs (already mostly does via info check)


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

  const paramsMap = useMemo(() => {
    const m = new Map<string, string[]>()
    data?.params?.forEach(([key, ps]) => m.set(JSON.stringify(key), ps || []))
    return m
  }, [data])

  const rootParams = data?.root_params || []

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
              {rootParams.length > 0 && (
                <span className="root-params" title="Parameters traced through the execution">
                  params: {rootParams.join(', ')}
                </span>
              )}
            </div>
            {data.body.length === 0 && (
              <div className="tree-empty">This function makes no tracked calls.</div>
            )}
            <BodySequence
              elems={data.body}
              graphMap={graphMap}
              paramsMap={paramsMap}
              depth={0}
              basePath="r"
              expanded={expanded}
              toggle={toggle}
              stack={new Set()}
              initialTraced={rootParams}
            />
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
