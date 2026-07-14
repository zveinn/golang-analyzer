import { useEffect, useRef, useState } from 'react'
import TraceView from './TraceView.jsx'

// The UI is a dumb renderer: the backend sends a structured trace tree and
// every semantic decision (kinds, labels, positions) was already made
// there. This side only maps kinds to styling. No code analysis here.

let nextTraceId = 1

function tagIds(node, counter) {
  node._id = counter.n++
  node.kids?.forEach((k) => tagIds(k, counter))
}

// ---- localStorage persistence ----

const STORAGE_KEY = 'code-analyzer.traces.v1'
const STORAGE_MAX_ITEMS = 30
const STORAGE_MAX_BYTES = 4_000_000

// dedupeKey identifies an envelope so restored storage and the server's
// history replay don't produce duplicates.
function dedupeKey(m) {
  return `${m.type}|${m.target}|${m.time}`
}

function prepare(msg) {
  msg.id = nextTraceId++
  if (msg.root) {
    const counter = { n: 0 }
    tagIds(msg.root, counter)
    msg.nodeCount = counter.n
  }
  return msg
}

function loadStored() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const arr = JSON.parse(raw)
    if (!Array.isArray(arr)) return []
    return arr.map(prepare)
  } catch {
    return []
  }
}

function saveStored(traces) {
  try {
    let list = traces.slice(0, STORAGE_MAX_ITEMS)
    let raw = JSON.stringify(list)
    // stay under the quota: drop oldest entries (list is newest-first)
    while (raw.length > STORAGE_MAX_BYTES && list.length > 1) {
      list = list.slice(0, list.length - 1)
      raw = JSON.stringify(list)
    }
    localStorage.setItem(STORAGE_KEY, raw)
  } catch {
    try {
      localStorage.removeItem(STORAGE_KEY)
    } catch {
      /* storage unavailable — view-only mode */
    }
  }
}

function clearStored() {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    /* storage unavailable */
  }
}

const initialTraces = loadStored()

export default function App() {
  const [traces, setTraces] = useState(initialTraces)
  const [selectedId, setSelectedId] = useState(initialTraces[0]?.id ?? null)
  const [status, setStatus] = useState('connecting')
  const followLatest = useRef(true)
  const seen = useRef(new Set(initialTraces.map(dedupeKey)))

  // persist the list whenever it changes
  useEffect(() => {
    saveStored(traces)
  }, [traces])

  useEffect(() => {
    let closed = false
    let retry
    let ws

    const connect = () => {
      ws = new WebSocket(`ws://${location.host}/ws`)
      ws.onopen = () => setStatus('connected')
      ws.onmessage = (ev) => {
        let msg
        try {
          msg = JSON.parse(ev.data)
        } catch {
          msg = { type: 'error', target: 'malformed message', text: String(ev.data) }
        }
        const key = dedupeKey(msg)
        if (seen.current.has(key)) return // already restored from storage
        seen.current.add(key)
        prepare(msg)
        setTraces((prev) => [msg, ...prev].slice(0, 100))
        if (followLatest.current) setSelectedId(msg.id)
      }
      ws.onclose = () => {
        setStatus('disconnected')
        if (!closed) retry = setTimeout(connect, 1000)
      }
    }

    connect()
    return () => {
      closed = true
      clearTimeout(retry)
      ws?.close()
    }
  }, [])

  const selected = traces.find((t) => t.id === selectedId) ?? null

  const select = (t) => {
    setSelectedId(t.id)
    followLatest.current = t.id === traces[0]?.id
  }

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <span className="logo">⌁</span>
          <span className="brand-name">code-analyzer</span>
          <span className={`status-dot ${status}`} title={`websocket ${status}`} />
        </div>
        <div className="sidebar-list">
          {traces.length === 0 && <div className="sidebar-empty">no traces yet</div>}
          {traces.map((t) => (
            <SidebarItem key={t.id} t={t} active={t.id === selectedId} onClick={() => select(t)} />
          ))}
        </div>
        {traces.length > 0 && (
          <button
            className="ghost clear-all"
            title="clears the list and local storage"
            onClick={() => {
              setTraces([])
              setSelectedId(null)
              followLatest.current = true
              seen.current = new Set()
              clearStored()
            }}
          >
            clear all + storage
          </button>
        )}
      </aside>
      <main className="main">
        {!selected && <EmptyState status={status} />}
        {selected && selected.type === 'error' && <ErrorView trace={selected} />}
        {selected && selected.type !== 'error' && <TraceView key={selected.id} trace={selected} />}
      </main>
    </div>
  )
}

function SidebarItem({ t, active, onClick }) {
  const target = t.target ?? ''
  const idx = target.lastIndexOf('/')
  const name = idx >= 0 ? target.slice(idx + 1) : target
  const dir = idx >= 0 ? target.slice(0, idx) : ''
  return (
    <button className={`sidebar-item ${active ? 'active' : ''} t-${t.type}`} onClick={onClick}>
      <div className="item-top">
        <span className="item-name">{name}</span>
        <span className="item-time">{t.time}</span>
      </div>
      {dir && <div className="item-dir">{dir}</div>}
      <div className="item-meta">
        {t.type === 'error' && <span className="chip chip-error">error</span>}
        {t.nodeCount > 0 && <span className="item-nodes">{t.nodeCount} nodes</span>}
        {t.params &&
          Object.entries(t.params).map(([k, v]) => (
            <span key={k} className="chip">
              {k}={v}
            </span>
          ))}
      </div>
    </button>
  )
}

function ErrorView({ trace }) {
  return (
    <div className="trace-view">
      <div className="trace-header">
        <span className="chip chip-error">error</span>
        <h2 className="trace-target">{trace.target}</h2>
        <span className="trace-time">{trace.time}</span>
      </div>
      <div className="error-panel">
        <pre>{trace.text}</pre>
      </div>
    </div>
  )
}

function EmptyState({ status }) {
  return (
    <div className="empty-state">
      <div className="empty-logo">⌁</div>
      <h2>waiting for traces</h2>
      <p>
        websocket: <span className={`status-text ${status}`}>{status}</span> · tcp intake on :1112
      </p>
      <pre>{`./client <file.go> <line> [param value]...

./client examples/main.go 36
./client examples/main.go 36 depth 10 expand all`}</pre>
    </div>
  )
}
