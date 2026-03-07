import React, { useEffect, useState } from 'react'
import FileUpload from '../components/FileUpload'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

export default function FilesPage({ username, password }) {
  const [files, setFiles] = useState([])
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState(null)

  const loadFiles = async () => {
    if (!username || !password) return
    setLoading(true)
    setMessage(null)
    try {
      const res = await fetch(`${API_BASE}/files?username=${username}`, {
        headers: { 'X-DFS-Password': password }
      })
      if (!res.ok) throw new Error(await res.text())
      const data = await res.json()
      setFiles(data.filenames || [])
    } catch (err) {
      setMessage({ type: 'danger', text: err.message })
    } finally { setLoading(false) }
  }

  useEffect(() => { if (username && password) loadFiles() }, [username, password])

  const onDelete = async (filename) => {
    if (!confirm(`Are you sure you want to delete "${filename}"?`)) return
    try {
      const res = await fetch(`${API_BASE}/files/${username}/${encodeURIComponent(filename)}`, {
        method: 'DELETE',
        headers: { 'X-DFS-Password': password }
      })
      if (!res.ok) throw new Error(await res.text())
      setMessage({ type: 'success', text: 'File deleted successfully' })
      loadFiles()
    } catch (err) {
      setMessage({ type: 'danger', text: err.message })
    }
  }

  const onDownload = (filename) => {
    // Append the password as a query parameter for browser direct download
    const url = `${API_BASE}/files/${username}/${encodeURIComponent(filename)}?password=${encodeURIComponent(password)}`;
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  return (
    <div className="animate__animated animate__fadeIn">
      <FileUpload username={username} password={password} onSuccess={loadFiles} />

      {message && (
        <div className={`alert alert-${message.type} alert-dismissible fade show rounded-3 border-0 shadow-sm mb-4`} role="alert">
          {message.text}
          <button type="button" className="btn-close" onClick={() => setMessage(null)}></button>
        </div>
      )}

      <div className="d-flex justify-content-between align-items-center mb-3 mt-5">
        <h4 className="fw-bold mb-0">Your Files</h4>
        <button
          className="btn btn-light btn-sm rounded-pill px-3 fw-bold border"
          onClick={loadFiles}
          disabled={loading}
        >
          {loading ? <span className="spinner-border spinner-border-sm me-2"></span> : <i className="bi bi-arrow-clockwise me-1"></i>}
          Refresh
        </button>
      </div>

      <div className="card border-0 shadow-sm rounded-4 overflow-hidden">
        <div className="list-group list-group-flush">
          {files.length === 0 && !loading && (
            <div className="list-group-item py-5 text-center text-muted">
              <i className="bi bi-folder2-open display-4 d-block mb-3 opacity-25"></i>
              <p className="mb-0">No files found. Start by uploading something!</p>
            </div>
          )}

          {loading && files.length === 0 && (
            <div className="list-group-item py-5 text-center text-muted">
              <span className="spinner-border text-primary mb-3"></span>
              <p className="mb-0">Loading your secure storage...</p>
            </div>
          )}

          {files.map(f => (
            <div className="list-group-item list-group-item-action d-flex justify-content-between align-items-center py-3 border-bottom transition-all" key={f}>
              <div className="d-flex align-items-center overflow-hidden">
                <div className="bg-light rounded-3 p-2 me-3 flex-shrink-0">
                  <i className={`bi ${getFileIcon(f)} text-primary fs-4`}></i>
                </div>
                <div className="text-truncate">
                  <h6 className="mb-0 text-truncate fw-semibold">{f}</h6>
                  <span className="text-muted small">Distributed across XORFS cluster</span>
                </div>
              </div>
              <div className="flex-shrink-0 ms-3">
                <button
                  className="btn btn-sm btn-outline-primary rounded-pill px-3 me-2 transition-all"
                  onClick={() => onDownload(f)}
                  title="Download File"
                >
                  <i className="bi bi-download"></i> <span className="d-none d-md-inline ms-1">Download</span>
                </button>
                <button
                  className="btn btn-sm btn-outline-danger rounded-pill px-3 transition-all"
                  onClick={() => onDelete(f)}
                  title="Delete File"
                >
                  <i className="bi bi-trash"></i>
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function getFileIcon(filename) {
  const ext = filename.split('.').pop().toLowerCase();
  switch (ext) {
    case 'pdf': return 'bi-file-earmark-pdf';
    case 'doc': case 'docx': return 'bi-file-earmark-word';
    case 'xls': case 'xlsx': return 'bi-file-earmark-excel';
    case 'jpg': case 'jpeg': case 'png': case 'gif': return 'bi-file-earmark-image';
    case 'zip': case 'rar': case '7z': return 'bi-file-earmark-zip';
    case 'txt': return 'bi-file-earmark-text';
    default: return 'bi-file-earmark';
  }
}
