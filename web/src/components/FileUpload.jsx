import React, { useState } from 'react'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

export default function FileUpload({ username, password, onSuccess }) {
  const [file, setFile] = useState(null)
  const [filename, setFilename] = useState('')
  const [uploading, setUploading] = useState(false)
  const [msg, setMsg] = useState(null)

  const submit = async (e) => {
    e.preventDefault()
    if (!file) { setMsg({ type: 'danger', text: 'Please select a file first' }); return }

    const fd = new FormData()
    fd.append('file', file)
    fd.append('username', username)
    if (filename.trim()) fd.append('filename', filename.trim())

    setUploading(true); setMsg(null)
    try {
      const res = await fetch(`${API_BASE}/files`, {
        method: 'POST',
        body: fd,
        headers: { 'X-DFS-Password': password }
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.message || 'Upload failed')

      setFile(null);
      setFilename('');
      setMsg({ type: 'success', text: 'File uploaded successfully!' })
      if (onSuccess) onSuccess(data)
    } catch (err) {
      setMsg({ type: 'danger', text: err.message })
    }
    setUploading(false)
  }

  return (
    <div className="card border-0 shadow-sm rounded-4 overflow-hidden mb-4">
      <div className="card-header bg-white py-3 border-0">
        <h5 className="mb-0 fw-bold"><i className="bi bi-cloud-arrow-up me-2 text-primary"></i>Upload New File</h5>
      </div>
      <div className="card-body p-4">
        <form onSubmit={submit}>
          <div className="row">
            <div className="col-md-6 mb-3">
              <label className="form-label small fw-bold text-muted">Select File</label>
              <input
                type="file"
                className="form-control form-control-lg rounded-3 fs-6"
                onChange={e => setFile(e.target.files?.[0] || null)}
              />
            </div>
            <div className="col-md-6 mb-3">
              <label className="form-label small fw-bold text-muted">Custom Filename (Optional)</label>
              <input
                className="form-control form-control-lg rounded-3 fs-6"
                value={filename}
                onChange={e => setFilename(e.target.value)}
                placeholder="Leave blank to use original"
              />
            </div>
          </div>

          <div className="d-flex align-items-center justify-content-between mt-2">
            <div>
              {msg && <div className={`alert alert-${msg.type} py-2 px-3 mb-0 rounded-3 small animate__animated animate__fadeIn`}>{msg.text}</div>}
            </div>
            <button className="btn btn-primary px-4 py-2 rounded-3 fw-bold transition-all" disabled={uploading}>
              {uploading ? (
                <>
                  <span className="spinner-border spinner-border-sm me-2" role="status" aria-hidden="true"></span>
                  Uploading...
                </>
              ) : (
                'Upload File'
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
