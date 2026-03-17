import React, { useEffect, useState } from 'react'
import FileUpload from '../components/FileUpload'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

// Map file extensions to Bootstrap Icon classes
function fileIcon(filename) {
  const ext = (filename.split('.').pop() || '').toLowerCase()
  const map = {
    pdf: 'bi-file-pdf-fill text-danger',
    jpg: 'bi-file-image-fill text-primary', jpeg: 'bi-file-image-fill text-primary',
    png: 'bi-file-image-fill text-primary', gif: 'bi-file-image-fill text-primary',
    svg: 'bi-file-image-fill text-info',
    mp4: 'bi-file-play-fill text-warning', avi: 'bi-file-play-fill text-warning',
    mov: 'bi-file-play-fill text-warning', mkv: 'bi-file-play-fill text-warning',
    mp3: 'bi-file-music-fill text-info', wav: 'bi-file-music-fill text-info',
    zip: 'bi-file-zip-fill text-secondary', tar: 'bi-file-zip-fill text-secondary',
    gz: 'bi-file-zip-fill text-secondary', rar: 'bi-file-zip-fill text-secondary',
    txt: 'bi-file-text-fill text-muted', md: 'bi-file-text-fill text-muted',
    doc: 'bi-file-word-fill text-primary', docx: 'bi-file-word-fill text-primary',
    xls: 'bi-file-excel-fill text-success', xlsx: 'bi-file-excel-fill text-success',
    ppt: 'bi-file-ppt-fill text-orange', pptx: 'bi-file-ppt-fill text-orange',
    js: 'bi-file-code-fill text-warning', ts: 'bi-file-code-fill text-primary',
    jsx: 'bi-file-code-fill text-info', tsx: 'bi-file-code-fill text-info',
    py: 'bi-file-code-fill text-success', go: 'bi-file-code-fill text-cyan',
    c: 'bi-file-code-fill text-muted', cpp: 'bi-file-code-fill text-muted',
    json: 'bi-file-code-fill text-warning', yaml: 'bi-file-code-fill text-warning',
    html: 'bi-filetype-html text-danger', css: 'bi-filetype-css text-primary',
  }
  return map[ext] || 'bi-file-earmark-fill text-secondary'
}

export default function FilesPage({ authToken, username }) {
  const [files, setFiles] = useState([])
  const [loading, setLoading] = useState(false)
  const [toasts, setToasts] = useState([])
  const [deletingFile, setDeletingFile] = useState(null)
  const [downloadingFile, setDownloadingFile] = useState(null)
  const [downloadProgress, setDownloadProgress] = useState({})

  const authHeaders = { Authorization: `Bearer ${authToken}` }

  const addToast = (type, message) => {
    const id = Date.now() + Math.random()
    setToasts(prev => [...prev, { id, type, message }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 5000)
  }

  const loadFiles = async () => {
    setLoading(true)
    try {
      const res = await fetch(`${API_BASE}/files`, { headers: authHeaders })
      const data = await res.json()
      if (!res.ok) throw new Error(data.message || 'Failed to load files')
      setFiles(data.filenames || [])
    } catch (err) {
      addToast('danger', err.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { loadFiles() }, [authToken])

  const onDelete = async (filename) => {
    if (!window.confirm(`Delete "${filename}"? This cannot be undone.`)) return
    setDeletingFile(filename)
    try {
      const res = await fetch(`${API_BASE}/files/delete/${encodeURIComponent(filename)}`, {
        method: 'DELETE', headers: authHeaders
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.message || 'Delete failed')
      addToast('success', `"${filename}" deleted`)
      loadFiles()
    } catch (err) {
      addToast('danger', err.message)
    } finally {
      setDeletingFile(null)
    }
  }

  const onDownload = (filename) => {
    setDownloadingFile(filename)
    setDownloadProgress(prev => ({ ...prev, [filename]: 0 }))
    
    const xhr = new XMLHttpRequest()
    
    xhr.addEventListener('progress', (e) => {
      if (e.lengthComputable) {
        const percentComplete = (e.loaded / e.total) * 100
        setDownloadProgress(prev => ({ ...prev, [filename]: percentComplete }))
      }
    })
    
    xhr.addEventListener('load', () => {
      if (xhr.status === 200) {
        const blob = xhr.response
        const url = window.URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = filename
        document.body.appendChild(a)
        a.click()
        window.URL.revokeObjectURL(url)
        document.body.removeChild(a)
        addToast('success', `"${filename}" downloaded`)
      } else {
        addToast('danger', 'Download failed')
      }
      setDownloadingFile(null)
      setDownloadProgress(prev => { const newState = { ...prev }; delete newState[filename]; return newState })
    })
    
    xhr.addEventListener('error', () => {
      addToast('danger', 'Download error')
      setDownloadingFile(null)
      setDownloadProgress(prev => { const newState = { ...prev }; delete newState[filename]; return newState })
    })
    
    xhr.open('GET', `${API_BASE}/files/download/${encodeURIComponent(filename)}`)
    xhr.setRequestHeader('Authorization', authHeaders.Authorization)
    xhr.responseType = 'blob'
    xhr.send()
  }

  return (
    <>
      {/* Toast container */}
      <div className="toast-container position-fixed top-0 end-0 p-3" style={{ zIndex: 1100 }}>
        {toasts.map(t => (
          <div key={t.id}
            className={`toast show align-items-center text-white border-0 mb-2`}
            style={{ background: t.type === 'success' ? '#198754' : '#dc3545', minWidth: 280 }}
            role="alert">
            <div className="d-flex">
              <div className="toast-body d-flex align-items-center gap-2">
                <i className={`bi ${t.type === 'success' ? 'bi-check-circle-fill' : 'bi-exclamation-circle-fill'}`}></i>
                {t.message}
              </div>
              <button type="button" className="btn-close btn-close-white me-2 m-auto"
                onClick={() => setToasts(prev => prev.filter(x => x.id !== t.id))} />
            </div>
          </div>
        ))}
      </div>

      {/* Upload card */}
      <div className="card border-0 shadow-sm mb-4">
        <div className="card-header bg-white border-bottom d-flex align-items-center gap-2 py-3">
          <i className="bi bi-cloud-upload-fill text-primary fs-5"></i>
          <span className="fw-semibold">Upload File</span>
        </div>
        <div className="card-body">
          <FileUpload
            authToken={authToken}
            onSuccess={() => { addToast('success', 'File uploaded successfully'); loadFiles() }}
            onError={(msg) => addToast('danger', msg)}
          />
        </div>
      </div>

      {/* Files card */}
      <div className="card border-0 shadow-sm">
        <div className="card-header bg-white border-bottom d-flex justify-content-between align-items-center py-3">
          <div className="d-flex align-items-center gap-2">
            <i className="bi bi-folder2-open text-warning fs-5"></i>
            <span className="fw-semibold">Your Files</span>
            {files.length > 0 && (
              <span className="badge rounded-pill" style={{ background: '#e9ecef', color: '#495057' }}>
                {files.length}
              </span>
            )}
          </div>
          <button className="btn btn-sm btn-outline-secondary d-flex align-items-center gap-1"
            onClick={loadFiles} disabled={loading}>
            {loading
              ? <><span className="spinner-border spinner-border-sm"></span><span>Loading</span></>
              : <><i className="bi bi-arrow-clockwise"></i><span>Refresh</span></>}
          </button>
        </div>

        <div className="card-body p-0">
          {loading && files.length === 0 ? (
            <div className="text-center py-5 text-muted">
              <div className="spinner-border mb-3" role="status"></div>
              <p className="mb-0">Loading your files…</p>
            </div>
          ) : files.length === 0 ? (
            <div className="text-center py-5" style={{ color: '#adb5bd' }}>
              <i className="bi bi-folder2-open" style={{ fontSize: '3.5rem', opacity: 0.35 }}></i>
              <p className="mt-3 mb-1 fw-medium" style={{ color: '#6c757d' }}>No files yet</p>
              <p className="small mb-0" style={{ color: '#adb5bd' }}>Upload your first file using the panel above.</p>
            </div>
          ) : (
            <div className="table-responsive">
              <table className="table table-hover align-middle mb-0">
                <thead>
                  <tr style={{ background: '#f8f9fa', fontSize: '0.78rem', textTransform: 'uppercase', letterSpacing: '0.04em', color: '#6c757d' }}>
                    <th style={{ width: 48 }} className="ps-4"></th>
                    <th colSpan="2">Filename</th>
                  </tr>
                </thead>
                <tbody>
                  {files.map(f => (
                    <React.Fragment key={f}>
                      <tr>
                        <td className="ps-4">
                          <i className={`bi ${fileIcon(f)} fs-5`}></i>
                        </td>
                        <td colSpan="2">
                          <span className="fw-medium" style={{ wordBreak: 'break-all' }}>{f}</span>
                        </td>
                      </tr>
                      <tr>
                        <td colSpan="2" className="ps-4 py-2">
                          <div className="d-flex justify-content-between align-items-center">
                            {downloadingFile === f && downloadProgress[f] !== undefined ? (
                              <div style={{ flex: 1, marginRight: '0.5rem' }}>
                                <div className="progress" style={{ height: '1.5rem' }}>
                                  <div
                                    className="progress-bar progress-bar-striped progress-bar-animated"
                                    style={{ width: `${downloadProgress[f]}%` }}
                                    role="progressbar"
                                    aria-valuenow={downloadProgress[f]}
                                    aria-valuemin="0"
                                    aria-valuemax="100">
                                    {Math.round(downloadProgress[f])}%
                                  </div>
                                </div>
                              </div>
                            ) : (
                              <button
                                className="btn btn-sm btn-outline-primary"
                                onClick={() => onDownload(f)}
                                disabled={downloadingFile !== null}
                                title="Download">
                                <i className="bi bi-download me-1"></i>Download
                              </button>
                            )}
                            <button
                              className="btn btn-sm btn-outline-danger"
                              onClick={() => onDelete(f)}
                              disabled={deletingFile === f}
                              title="Delete">
                              {deletingFile === f
                                ? <span className="spinner-border spinner-border-sm"></span>
                                : <><i className="bi bi-trash me-1"></i>Delete</>}
                            </button>
                          </div>
                        </td>
                      </tr>
                    </React.Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>


    </>
  )
}
