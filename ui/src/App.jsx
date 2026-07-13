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

export default function App() {
  const [traces, setTraces] = useState([])
  const [selectedId, setSelectedId] = useState(null)
  const [status, setStatus] = useState('connecting')
  const followLatest = useRef(true)

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
        msg.id = nextTraceId++
        if (msg.root) {
          const counter = { n: 0 }
          tagIds(msg.root, counter)
          msg.nodeCount = counter.n
        }
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
            onClick={() => {
              setTraces([])
              setSelectedId(null)
              followLatest.current = true
            }}
          >
            clear all
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
