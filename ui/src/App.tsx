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
  receiver?: string
}

interface BodyElement {
  kind: 'Call' | 'Loop' | 'Def' | 'ChannelSend' | 'ChannelRecv' | 'Callback'
  info?: CallInfo
  body?: BodyElement[]
  name?: string
  rhs?: string
  channel?: string
  value?: string
  target?: string | null
  ownerType?: string | null
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

/** Strip generics and package to get base type name for matching (e.g. "*queueTarget[foo]" -> "queueTarget"). */
function baseTypeName(typ: string | null | undefined): string | null {
  if (!typ) return null
  let t = typ.trim().replace(/^\*/, '')
  const bracket = t.indexOf('[')
  if (bracket >= 0) t = t.slice(0, bracket)
  const dot = t.lastIndexOf('.')
  if (dot >= 0) t = t.slice(dot + 1)
  return t || null
}

/**
 * Extract whole identifiers referenced by an argument expression or receiver expression.
 * - Strips string literals ("..." and `...` and runes) so that words inside strings
 *   (e.g. "code" inside a log message) are ignored.
 * - Returns only syntactic identifiers (e.g. "FR", "url", "err" from FR.Server.GetURL(url) or err).
 */
function extractIdentifiersFromExpr(expr: string | undefined): string[] {
  if (!expr) return []
  let s = expr.trim()

  // Strip double-quoted strings (with basic escape handling)
  s = s.replace(/"(?:\\.|[^"\\])*"/g, ' ')
  // Strip raw string literals
  s = s.replace(/`[^`]*`/g, ' ')
  // Strip rune literals 'a' or '\n'
  s = s.replace(/'(?:\\.|[^'\\])+'/g, ' ')

  const ids: string[] = []
  const re = /\b([a-zA-Z_][a-zA-Z0-9_]*)\b/g
  let m: RegExpExecArray | null
  while ((m = re.exec(s)) !== null) {
    const id = m[1]
    if (id !== 'true' && id !== 'false' && id !== 'nil' && id !== 'iota') {
      ids.push(id)
    }
  }
  return ids
}

/**
 * Return the live names that are *actually passed* into this specific call,
 * either via its arguments or its receiver expression.
 * Only exact whole identifiers after stripping strings count.
 */
function getPassedLiveNames(info: CallInfo | undefined, liveNames: string[]): string[] {
  if (!info || liveNames.length === 0) return []
  const exprs: string[] = []
  if (info.receiver) exprs.push(info.receiver)
  if (info.args && info.args.length) exprs.push(...info.args)

  const passed = new Set<string>()
  for (const e of exprs) {
    for (const id of extractIdentifiersFromExpr(e)) {
      passed.add(id)
    }
  }
  return liveNames.filter((nm) => passed.has(nm))
}

/* ------------------------------------------------------------------ */
/*  BodySequence: renders a list of BodyElements while simulating     */
/*  parameter/variable flow for the current frame (any Def-created    */
/*  var that flows into calls is tracked and labeled "param").        */
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
  goroutineLaunched,
  hideNonLocal,
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
  goroutineLaunched?: Set<string>
  hideNonLocal?: boolean
}) {
  const nodes: React.ReactNode[] = []
  const live = new Set<string>(initialTraced || [])

  elems.forEach((elem, i) => {
    const path = `${basePath}/${i}`

    if (elem.kind === 'Def') {
      // Any variable defined in the execution chain is now live for downstream tracing.
      // If it (or a derived value) is later passed as an argument or used as a receiver
      // into another call, we want to label the use as "param" (not external).
      if (elem.name) {
        live.add(elem.name)
      }
      // Defs are not rendered visibly (they exist only to drive tracing).
      return
    }

    if (elem.kind === 'ChannelSend' || elem.kind === 'ChannelRecv') {
      // Render via TreeNode (which has special cases); no change to live traced names for now.
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
          goroutineLaunched={goroutineLaunched}
          hideNonLocal={hideNonLocal}
        />
      )
      return
    }

    if (elem.kind === 'Callback') {
      const children = elem.body ?? []
      const hasContent = children.some((c: any) => {
        if (c.kind === 'Call' && hideNonLocal && c.info) {
          const k = describeCallee(c.info.callee).kind
          if (k === 'external' || k === 'stdlib') return false
        }
        return c.kind === 'Call' || c.kind === 'Loop' || c.kind === 'Callback' || 
          (c.kind === 'ChannelSend' || c.kind === 'ChannelRecv')
      })
      if (hasContent) {
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
            goroutineLaunched={goroutineLaunched}
            hideNonLocal={hideNonLocal}
          />
        )
      }
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
          goroutineLaunched={goroutineLaunched}
          hideNonLocal={hideNonLocal}
        />
      )
      return
    }

    if (elem.kind === 'Call' && elem.info) {
      const { kind } = describeCallee(elem.info.callee)
      if (hideNonLocal && (kind === 'external' || kind === 'stdlib')) {
        return // skip printing this call (user toggle)
      }

      // Only names that are *actually passed* as arguments or receiver to *this* call.
      // This prevents showing unrelated live vars (like "er" or "code" from strings).
      const carriesHere = getPassedLiveNames(elem.info, Array.from(live))

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
          goroutineLaunched={goroutineLaunched}
          hideNonLocal={hideNonLocal}
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
  goroutineLaunched,
  hideNonLocal,
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
  goroutineLaunched?: Set<string>
  hideNonLocal?: boolean
}) {
  const open = expanded.has(path)

  /* ---- Callback (anonymous func passed to higher-order func) ---- */
  if (elem.kind === 'Callback') {
    const children = elem.body ?? []
    const hasContent = children.some((c: any) => {
      if (c.kind === 'Call' && hideNonLocal && c.info) {
        const k = describeCallee(c.info.callee).kind
        if (k === 'external' || k === 'stdlib') return false
      }
      return c.kind === 'Call' || c.kind === 'Loop' || c.kind === 'Callback' || 
        (c.kind === 'ChannelSend' || c.kind === 'ChannelRecv')
    })
    if (!hasContent) return null
    const cbName = elem.name || 'callback'
    return (
      <div className="tree-branch">
        <div
          className={`row row-loop ${open ? 'is-open' : ''}`}
          style={{ ['--depth' as string]: depth }}
          onClick={() => toggle(path)}
        >
          <Gutter depth={depth} expandable open={open} />
          <span className="row-icon loop">ƒ</span>
          <span className="row-label loop">callback</span>
          <span className="badge badge-loop">{cbName}</span>
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
              goroutineLaunched={goroutineLaunched}
              hideNonLocal={hideNonLocal}
            />
          </div>
        )}
      </div>
    )
  }

  /* ---- Channel ops ---- */
  if (elem.kind === 'ChannelSend') {
    const sendOwnerBase = elem.ownerType ? baseTypeName(elem.ownerType) : null
    const sendChan = (elem.channel || '').trim()
    const chBase = sendChan.split('.').pop() || sendChan

    const readers: string[] = []
    graphMap.forEach((body, keyStr) => {
      const walk = (els: BodyElement[]) => {
        for (const el of els || []) {
          if (el.kind === 'ChannelRecv' && el.channel) {
            const recvOwnerBase = el.ownerType ? baseTypeName(el.ownerType) : null
            const rbase = el.channel.split('.').pop() || ''
            const recvChan = (el.channel || '').trim()
            let k: FuncKey | null = null
            try { k = JSON.parse(keyStr) } catch {}

            const funcRecvBase = k?.recv ? baseTypeName(k.recv) : null

            const exactChanMatch = recvChan === sendChan   // strong signal for same channel var (e.g. global LogQueue)

            let matches = false

            if (sendOwnerBase) {
              // Exact match using resolved owner type of the channel base.
              // A send on obj.field where obj has type *Foo will match recvs whose owner or
              // enclosing method receiver base type matches *Foo.
              if (recvOwnerBase === sendOwnerBase || funcRecvBase === sendOwnerBase) {
                matches = true
              }
            } else if (exactChanMatch) {
              matches = true
            } else if (funcRecvBase) {
              // Fallback for when owner not resolved: field name match but only inside methods.
              if (rbase === chBase) {
                matches = true
              }
            } else if (!sendOwnerBase && !recvOwnerBase) {
              matches = rbase === chBase
            }

            if (matches && k) {
              const keyStr2 = JSON.stringify(k)
              // Include if exact channel match (globals etc), or if it's a goroutine-launched reader.
              if (exactChanMatch || goroutineLaunched?.has(keyStr2)) {
                const disp = formatFuncKey(k)
                if (!readers.includes(disp)) readers.push(disp)
              }
            }
          }
          if (el.kind === 'Loop' && el.body) walk(el.body)
        }
      }
      walk(body)
    })

    // Just use the matched readers. The matching logic above already tries to be precise.
    const finalReaders = readers

    return (
      <div className="tree-branch">
        <div
          className="row"
          style={{ ['--depth' as string]: depth }}
        >
          <Gutter depth={depth} />
          <span className="kind-dot kind-external" />
          <span className="row-name">
            <span className="qual-pkg">[chan send] </span>
            <span className="ident">{elem.channel}</span>
          </span>
          <span className="row-tags">
            <span className="badge badge-external">send</span>
          </span>
          {elem.value && <span className="row-count">← {elem.value}</span>}
        </div>
        {finalReaders.length > 0 && (
          <div className="children" style={{ paddingLeft: '1.5em', fontSize: '0.9em', opacity: 0.85 }}>
            {finalReaders.map((r, i) => (
              <div key={i} style={{ color: '#34d399' }}>└── [chan reader] {r}</div>
            ))}
          </div>
        )}
      </div>
    )
  }
  if (elem.kind === 'ChannelRecv') {
    return (
      <div className="tree-branch">
        <div
          className="row"
          style={{ ['--depth' as string]: depth }}
        >
          <Gutter depth={depth} />
          <span className="kind-dot kind-stdlib" />
          <span className="row-name">
            <span className="qual-pkg">[chan recv] </span>
            <span className="ident">{elem.channel}</span>
          </span>
          <span className="row-tags">
            <span className="badge badge-stdlib">recv</span>
          </span>
          {elem.target && <span className="row-count">→ {elem.target}</span>}
        </div>
      </div>
    )
  }

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
              goroutineLaunched={goroutineLaunched || new Set()}
              hideNonLocal={hideNonLocal}
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

    if (hideNonLocal && (kind === 'external' || kind === 'stdlib')) {
      return null // respect the hide toggle
    }

    const keyStr = key ? JSON.stringify(key) : null
    const subBody = keyStr ? graphMap.get(keyStr) : undefined
    const isRecursive = keyStr ? stack.has(keyStr) : false
    const expandable = !!subBody && subBody.length > 0 && !isRecursive
    const nextStack = keyStr ? new Set(stack).add(keyStr) : stack

    // Only names actually passed (args + receiver) to *this* call get the param badge.
    const localCarries = carries || []
    const effectiveCarries = localCarries.length
      ? localCarries
      : getPassedLiveNames(info, traced || [])

    const showParamBadge = kind !== 'stdlib' && effectiveCarries.length > 0

    // Map to child formals using identifiers present in the actual passed arg expressions.
    // If an arg expression references any live name(s), the corresponding formal in the
    // callee becomes live inside the child (so its uses inside will be labeled param too).
    let childInitialTraced: string[] | undefined
    if (keyStr && subBody) {
      const childFormals = paramsMap.get(keyStr) || []
      const mapped: string[] = []
      const argList = info.args || []
      for (let i = 0; i < argList.length; i++) {
        const ids = extractIdentifiersFromExpr(argList[i])
        if (ids.some((id) => (traced || []).includes(id)) && childFormals[i]) {
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
            {showParamBadge && (
              <span className="badge badge-traced" title={`Param flow from: ${effectiveCarries.join(', ')}`}>
                param:{effectiveCarries.join(',')}
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
              goroutineLaunched={goroutineLaunched || new Set()}
              hideNonLocal={hideNonLocal}
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
      } else if (elem.kind === 'Callback' && elem.body?.length) {
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
  const [hideNonLocal, setHideNonLocal] = useState(false)  // toggle to hide stdlib + external calls

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

  // Functions that are launched as goroutines (targets of `go` calls). Channel readers are typically in such goroutines.
  const goroutineLaunched = useMemo(() => {
    const set = new Set<string>()
    if (!data) return set
    const collect = (els: BodyElement[]) => {
      for (const el of els) {
        if (el.kind === 'Call' && el.info?.goroutine && 'Local' in el.info.callee) {
          set.add(JSON.stringify(el.info.callee.Local))
        }
        if (el.kind === 'Loop' && el.body) collect(el.body)
      }
    }
    collect(data.body)
    data.graph.forEach(([, b]) => collect(b))
    return set
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
            <label className="filter-toggle" title="Hide stdlib and external calls from the tree (keeps local structure and traced params)">
              <input
                type="checkbox"
                checked={hideNonLocal}
                onChange={(e) => setHideNonLocal(e.target.checked)}
              />
              <span>Hide stdlib/external</span>
            </label>
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
              goroutineLaunched={goroutineLaunched || new Set()}
              hideNonLocal={hideNonLocal}
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
