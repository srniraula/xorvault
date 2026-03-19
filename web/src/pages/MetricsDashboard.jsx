import React, { useEffect, useState, useCallback } from 'react'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

function fmtMs(ms) {
  if (!ms && ms !== 0) return '—'
  if (ms >= 1000) return (ms / 1000).toFixed(2) + ' s'
  return ms.toFixed(1) + ' ms'
}
function fmtMBs(v) {
  if (!v) return '—'
  return v.toFixed(2) + ' MB/s'
}
function fmtSize(bytes) {
  if (!bytes) return '—'
  if (bytes >= 1e9) return (bytes / 1e9).toFixed(2) + ' GB'
  if (bytes >= 1e6) return (bytes / 1e6).toFixed(1) + ' MB'
  if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return bytes + ' B'
}

const OP_COLORS = {
  upload:   { bg: '#e7f3ff', text: '#0d6efd', badge: 'primary' },
  download: { bg: '#e8f5e9', text: '#198754', badge: 'success' },
  delete:   { bg: '#fdecea', text: '#dc3545', badge: 'danger' },
  ls:       { bg: '#fff8e1', text: '#f59e0b', badge: 'warning' },
}

function OpBadge({ op }) {
  const c = OP_COLORS[op] || { badge: 'secondary' }
  return <span className={`badge bg-${c.badge} bg-opacity-25 text-${c.badge}`}
    style={{ fontWeight: 600, fontSize: '0.72rem' }}>{op}</span>
}

function StatCard({ label, value, sub, color = '#0d6efd' }) {
  return (
    <div className="card border-0 shadow-sm h-100">
      <div className="card-body">
        <p className="small text-muted mb-1">{label}</p>
        <p className="fw-bold mb-0" style={{ fontSize: '1.5rem', color }}>{value}</p>
        {sub && <p className="small text-muted mb-0 mt-1">{sub}</p>}
      </div>
    </div>
  )
}

export default function MetricsDashboard({ authToken }) {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [filterOp, setFilterOp] = useState('all')
  const [filterUser, setFilterUser] = useState('all')
  const [filterSource, setFilterSource] = useState('all')

  const fetchMetrics = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/metrics`, {
        headers: { Authorization: `Bearer ${authToken}` }
      })
      if (!res.ok) throw new Error('Failed to fetch metrics')
      const json = await res.json()
      setData(json)
      setError('')
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [authToken])

  // Initial load
  useEffect(() => { fetchMetrics() }, [fetchMetrics])

  // Auto-refresh every 3 seconds
  useEffect(() => {
    if (!autoRefresh) return
    const id = setInterval(fetchMetrics, 3000)
    return () => clearInterval(id)
  }, [autoRefresh, fetchMetrics])

  const downloadCSV = () => {
    const a = document.createElement('a')
    a.href = `${API_BASE}/metrics/csv`
    a.setAttribute('download', 'dfs_metrics.csv')
    // Need auth header — use XHR
    const xhr = new XMLHttpRequest()
    xhr.open('GET', `${API_BASE}/metrics/csv`)
    xhr.setRequestHeader('Authorization', `Bearer ${authToken}`)
    xhr.responseType = 'blob'
    xhr.onload = () => {
      const url = URL.createObjectURL(xhr.response)
      const a = document.createElement('a')
      a.href = url; a.download = 'dfs_metrics.csv'
      document.body.appendChild(a); a.click()
      URL.revokeObjectURL(url); document.body.removeChild(a)
    }
    xhr.send()
  }

  if (loading) return (
    <div className="text-center py-5">
      <div className="spinner-border text-primary" role="status"></div>
      <p className="mt-3 text-muted">Loading metrics…</p>
    </div>
  )

  if (error) return (
    <div className="alert alert-danger">
      <i className="bi bi-exclamation-triangle me-2"></i>{error}
    </div>
  )

  if (!data) return null

  const { summary, recent, active_users, active_user_count } = data

  // Collect unique users and filter recent
  const allUsers = [...new Set(recent.map(m => m.username).filter(Boolean))]
  const filtered = recent.filter(m => {
    if (filterOp !== 'all' && m.operation !== filterOp) return false
    if (filterUser !== 'all' && m.username !== filterUser) return false
    if (filterSource !== 'all' && m.source !== filterSource) return false
    return true
  }).slice().reverse() // newest first

  return (
    <div>
      {/* Header bar */}
      <div className="d-flex justify-content-between align-items-center mb-4 flex-wrap gap-2">
        <div>
          <h5 className="fw-bold mb-0">
            <i className="bi bi-graph-up-arrow text-primary me-2"></i>
            Performance Metrics
          </h5>
          <p className="text-muted small mb-0">
            {data.total_recorded} operations recorded
            {active_user_count > 0 &&
              <span className="ms-2 badge bg-success bg-opacity-25 text-success">
                {active_user_count} user{active_user_count > 1 ? 's' : ''} active now
              </span>}
          </p>
        </div>
        <div className="d-flex gap-2 flex-wrap">
          <button
            className={`btn btn-sm ${autoRefresh ? 'btn-success' : 'btn-outline-secondary'}`}
            onClick={() => setAutoRefresh(v => !v)}>
            <i className={`bi ${autoRefresh ? 'bi-pause-fill' : 'bi-play-fill'} me-1`}></i>
            {autoRefresh ? 'Live' : 'Paused'}
          </button>
          <button className="btn btn-sm btn-outline-secondary" onClick={fetchMetrics}>
            <i className="bi bi-arrow-clockwise me-1"></i>Refresh
          </button>
          <button className="btn btn-sm btn-outline-primary" onClick={downloadCSV}>
            <i className="bi bi-download me-1"></i>Export CSV
          </button>
        </div>
      </div>

      {/* Active users */}
      {active_user_count > 0 && (
        <div className="alert alert-info d-flex align-items-center gap-3 py-2 mb-4">
          <i className="bi bi-people-fill fs-5"></i>
          <div>
            <strong>{active_user_count} user{active_user_count > 1 ? 's' : ''} currently operating:</strong>
            {' '}
            {Object.entries(active_users).map(([u, n]) =>
              <span key={u} className="badge bg-info text-dark me-1">
                {u} ({n})
              </span>
            )}
          </div>
        </div>
      )}

      {/* Summary stat cards */}
      <div className="row g-3 mb-4">
        <div className="col-6 col-md-3">
          <StatCard label="Total Operations" value={summary.total_ops}
            sub={`${summary.success_ops} success / ${summary.failed_ops} failed`} />
        </div>
        <div className="col-6 col-md-3">
          <StatCard label="Avg Latency" value={fmtMs(summary.avg_latency_ms)}
            sub="end-to-end" color="#f59e0b" />
        </div>
        <div className="col-6 col-md-3">
          <StatCard label="Avg Throughput" value={fmtMBs(summary.avg_throughput_mbps)}
            sub="uploads & downloads" color="#198754" />
        </div>
        <div className="col-6 col-md-3">
          <StatCard label="Total Data Moved" value={fmtSize(summary.total_bytes_transferred)}
            sub={`Max ${summary.max_concurrent_users_seen} concurrent users`} color="#6f42c1" />
        </div>
      </div>

      {/* Op type breakdown */}
      <div className="row g-3 mb-4">
        {[
          { label: 'Uploads',   val: summary.upload_ops,   color: '#0d6efd' },
          { label: 'Downloads', val: summary.download_ops, color: '#198754' },
          { label: 'Deletes',   val: summary.delete_ops,   color: '#dc3545' },
          { label: 'List ops',  val: summary.ls_ops,       color: '#f59e0b' },
          { label: 'Degraded downloads', val: summary.degraded_ops, color: '#6f42c1' },
        ].map(s => (
          <div key={s.label} className="col-6 col-md">
            <StatCard label={s.label} value={s.val} color={s.color} />
          </div>
        ))}
      </div>

      {/* Recent operations table */}
      <div className="card border-0 shadow-sm">
        <div className="card-header bg-white border-bottom d-flex justify-content-between align-items-center py-3 flex-wrap gap-2">
          <span className="fw-semibold">
            <i className="bi bi-clock-history text-muted me-2"></i>Recent Operations
          </span>
          {/* Filters */}
          <div className="d-flex gap-2 flex-wrap">
            <select className="form-select form-select-sm" style={{ width: 'auto' }}
              value={filterOp} onChange={e => setFilterOp(e.target.value)}>
              <option value="all">All ops</option>
              <option value="upload">Upload</option>
              <option value="download">Download</option>
              <option value="delete">Delete</option>
              <option value="ls">List</option>
            </select>
            <select className="form-select form-select-sm" style={{ width: 'auto' }}
              value={filterSource} onChange={e => setFilterSource(e.target.value)}>
              <option value="all">All sources</option>
              <option value="web">Web</option>
              <option value="cli">CLI</option>
            </select>
            {allUsers.length > 0 && (
              <select className="form-select form-select-sm" style={{ width: 'auto' }}
                value={filterUser} onChange={e => setFilterUser(e.target.value)}>
                <option value="all">All users</option>
                {allUsers.map(u => <option key={u} value={u}>{u}</option>)}
              </select>
            )}
          </div>
        </div>

        <div className="card-body p-0">
          {filtered.length === 0 ? (
            <div className="text-center py-5 text-muted">
              <i className="bi bi-inbox" style={{ fontSize: '2.5rem', opacity: 0.3 }}></i>
              <p className="mt-3 mb-0">No operations match the current filters.</p>
            </div>
          ) : (
            <div className="table-responsive">
              <table className="table table-hover align-middle mb-0" style={{ fontSize: '0.83rem' }}>
                <thead style={{ background: '#f8f9fa', fontSize: '0.73rem',
                  textTransform: 'uppercase', letterSpacing: '0.04em', color: '#6c757d' }}>
                  <tr>
                    <th className="ps-3">Op</th>
                    <th>File</th>
                    <th>User</th>
                    <th>Src</th>
                    <th>Size</th>
                    <th>Latency</th>
                    <th>Throughput</th>
                    <th>Bandwidth</th>
                    <th>Master OH</th>
                    <th>Parity</th>
                    <th>Chunks ✓</th>
                    <th>RAID</th>
                    <th>Concurrent</th>
                    <th>Status</th>
                    <th>Time</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((m, i) => {
                    const isError = !!m.error
                    const rowBg = isError ? '#fff5f5' : undefined
                    return (
                      <tr key={i} style={{ background: rowBg }}>
                        <td className="ps-3"><OpBadge op={m.operation} /></td>
                        <td style={{ maxWidth: 160, wordBreak: 'break-all' }}>
                          <span className="fw-medium">{m.filename || '/'}</span>
                        </td>
                        <td>
                          {m.username
                            ? <span className="badge bg-light text-dark border">{m.username}</span>
                            : <span className="text-muted">—</span>}
                        </td>
                        <td>
                          <span className={`badge ${m.source === 'web' ? 'bg-info' : 'bg-secondary'} bg-opacity-25 text-dark`}>
                            {m.source || '—'}
                          </span>
                        </td>
                        <td className="text-nowrap">{fmtSize(m.file_size_bytes)}</td>
                        <td className="text-nowrap">{fmtMs(m.total_latency_ms)}</td>
                        <td className="text-nowrap">{fmtMBs(m.throughput_mbps)}</td>
                        <td className="text-nowrap">{fmtMBs(m.bandwidth_mbps)}</td>
                        <td className="text-nowrap">
                          {m.master_overhead_pct > 0 ? m.master_overhead_pct.toFixed(1) + '%' : '—'}
                        </td>
                        <td className="text-nowrap">
                          {m.parity_compute_ms > 0 ? fmtMs(m.parity_compute_ms) : '—'}
                        </td>
                        <td className="text-nowrap">
                          {m.chunks_attempted > 0
                            ? `${m.chunks_succeeded}/${m.chunks_attempted}`
                            : '—'}
                        </td>
                        <td>
                          {m.degraded_download
                            ? <span className="badge bg-warning text-dark" title={`Recon: ${fmtMs(m.reconstruction_ms)}`}>
                                ⚠ {fmtMs(m.reconstruction_ms)}
                              </span>
                            : m.operation === 'download'
                              ? <span className="text-success small">✓ normal</span>
                              : <span className="text-muted">—</span>}
                        </td>
                        <td className="text-nowrap">
                          {m.concurrent_users > 0
                            ? <span className="badge bg-secondary bg-opacity-25 text-secondary">
                                {m.concurrent_users}
                              </span>
                            : '—'}
                        </td>
                        <td>
                          {isError
                            ? <span className="badge bg-danger" title={m.error}>✗ error</span>
                            : <span className="badge bg-success bg-opacity-25 text-success">✓</span>}
                        </td>
                        <td className="text-nowrap text-muted small">
                          {new Date(m.timestamp).toLocaleTimeString()}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
