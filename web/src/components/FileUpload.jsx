import React, { useState, useRef } from 'react'
import ChunkedUploader from '../utils/ChunkedUploader'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`
const CHUNKED_THRESHOLD = 10 * 1024 * 1024 // 10 MB

function fmtSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB'
  return (bytes / 1024 / 1024 / 1024).toFixed(1) + ' GB'
}

export default function FileUpload({ onSuccess, onError, authToken }) {
  const [file, setFile] = useState(null)
  const [filename, setFilename] = useState('')
  const [uploading, setUploading] = useState(false)
  const [progress, setProgress] = useState(0)
  const [dragOver, setDragOver] = useState(false)
  const inputRef = useRef(null)

  const pickFile = (f) => {
    if (!f) return
    setFile(f)
    setFilename('')
  }

  const handleDrop = (e) => {
    e.preventDefault()
    setDragOver(false)
    pickFile(e.dataTransfer.files[0])
  }

  const submit = async (e) => {
    e.preventDefault()
    if (!file) return
    setUploading(true)
    setProgress(0)
    const headers = { Authorization: `Bearer ${authToken}` }
    const name = filename.trim() || file.name
    try {
      let data
      if (file.size > CHUNKED_THRESHOLD) {
        const uploader = new ChunkedUploader(file, API_BASE, authToken)
        uploader.onProgress = (pct) => setProgress(Math.round(pct))
        data = await uploader.upload(name)
      } else {
        const fd = new FormData()
        fd.append('file', file)
        fd.append('filename', name)
        const res = await fetch(`${API_BASE}/files`, { method: 'POST', headers, body: fd })
        data = await res.json()
        if (!res.ok) throw new Error(data.message || 'Upload failed')
        setProgress(100)
      }
      setFile(null)
      setFilename('')
      setProgress(0)
      if (onSuccess) onSuccess(data)
    } catch (err) {
      setProgress(0)
      if (onError) onError(err.message)
    } finally {
      setUploading(false)
    }
  }

  const isLarge = file && file.size > CHUNKED_THRESHOLD

  return (
    <form onSubmit={submit}>
      {/* Drop zone */}
      <div
        onClick={() => !uploading && inputRef.current?.click()}
        onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
        onDragLeave={() => setDragOver(false)}
        onDrop={handleDrop}
        style={{
          border: `2px dashed ${dragOver ? '#0d6efd' : '#ced4da'}`,
          borderRadius: 12,
          padding: '2rem',
          textAlign: 'center',
          cursor: uploading ? 'default' : 'pointer',
          background: dragOver ? 'rgba(13,110,253,0.05)' : '#fafafa',
          transition: 'all 0.2s',
          userSelect: 'none',
        }}
      >
        <input
          ref={inputRef}
          type="file"
          className="d-none"
          onChange={e => pickFile(e.target.files?.[0])}
          disabled={uploading}
        />
        {file ? (
          <div>
            <i className="bi bi-file-earmark-check-fill text-success" style={{ fontSize: '2.2rem' }}></i>
            <p className="fw-semibold mb-1 mt-2" style={{ wordBreak: 'break-all' }}>{file.name}</p>
            <p className="text-muted small mb-0">
              {fmtSize(file.size)}
              {isLarge && <span className="badge bg-info text-dark ms-2">chunked upload</span>}
            </p>
          </div>
        ) : (
          <div>
            <i className="bi bi-cloud-arrow-up" style={{ fontSize: '2.5rem', color: '#adb5bd' }}></i>
            <p className="mt-2 mb-0" style={{ color: '#6c757d' }}>
              Drag &amp; drop a file here, or <span style={{ color: '#0d6efd', fontWeight: 500 }}>browse</span>
            </p>
          </div>
        )}
      </div>

      {/* Custom filename */}
      <div className="mt-3">
        <input
          className="form-control"
          placeholder="Save as… (optional, defaults to original filename)"
          value={filename}
          onChange={e => setFilename(e.target.value)}
          disabled={uploading}
        />
      </div>

      {/* Progress bar */}
      {uploading && (
        <div className="mt-3">
          <div className="d-flex justify-content-between small text-muted mb-1">
            <span>{isLarge ? 'Chunked upload in progress…' : 'Uploading…'}</span>
            <span>{progress || 0}%</span>
          </div>
          <div className="progress" style={{ height: 8 }}>
            <div
              className="progress-bar progress-bar-striped progress-bar-animated"
              style={{ width: `${Math.max(progress || 0, 5)}%` }}
            />
          </div>
        </div>
      )}

      {/* Submit */}
      <button
        type="submit"
        className="btn btn-primary w-100 mt-3 d-flex justify-content-center align-items-center gap-2"
        disabled={uploading || !file}
        style={{ height: 44 }}
      >
        {uploading
          ? <><span className="spinner-border spinner-border-sm"></span>Uploading {progress || 0}%</>
          : <><i className="bi bi-upload"></i>Upload</>}
      </button>
    </form>
  )
}
