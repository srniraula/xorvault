import React, { useEffect, useState } from 'react'
import FileUpload from '../components/FileUpload'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

export default function FilesPage({ authToken, userId }) {
  const [files, setFiles] = useState([])
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState(null)

  const getAuthHeaders = () => ({
    'Authorization': `Bearer ${authToken}`,
    'Content-Type': 'application/json'
  });

  const loadFiles = async () => {
    setLoading(true)
    setMessage(null)
    try {
      const res = await fetch(`${API_BASE}/files`, {
        headers: getAuthHeaders()
      });
      if (!res.ok) {
        const errorText = await res.text();
        throw new Error(errorText);
      }
      const data = await res.json()
      setFiles(data.filenames || [])
    } catch (err) {
      setMessage({ type: 'danger', text: err.message })
    } finally { 
      setLoading(false) 
    }
  }

  useEffect(() => { 
    loadFiles() 
  }, [authToken])

  const onUploadSuccess = (data) => {
    setMessage({ type: 'success', text: 'Upload succeeded' })
    loadFiles()
  }

  const onDelete = async (filename) => {
    if (!confirm(`Delete ${filename}?`)) return
    try {
      // Use the simplified delete endpoint that doesn't require clientId in URL
      const deleteRes = await fetch(`${API_BASE}/files/delete/${encodeURIComponent(filename)}`, { 
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      
      if (!deleteRes.ok) throw new Error(await deleteRes.text());
      
      setMessage({ type: 'success', text: 'Deleted successfully' })
      loadFiles()
    } catch (err) { 
      setMessage({ type: 'danger', text: err.message }) 
    }
  }

  const onDownload = async (filename) => {
    try {
      // Use the new simplified download endpoint
      const response = await fetch(`${API_BASE}/files/download/${encodeURIComponent(filename)}`, {
        headers: {
          'Authorization': `Bearer ${authToken}`
        }
      });
      
      if (!response.ok) {
        throw new Error('Download failed');
      }
      
      // Create blob and download
      const blob = await response.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.style.display = 'none';
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);
    } catch (err) {
      setMessage({ type: 'danger', text: 'Download failed: ' + err.message });
    }
  }

  return (
    <div>
      <div className="mb-3 d-flex gap-2 align-items-center">
        <div className="badge bg-success">Authenticated User</div>
        <button className="btn btn-outline-primary" onClick={loadFiles} disabled={loading}>
          {loading ? 'Loading...' : 'Refresh Files'}
        </button>
      </div>

      <FileUpload onSuccess={onUploadSuccess} authToken={authToken} />

      {message && <div className={`alert alert-${message.type} mt-3`}>{message.text}</div>}

      <h3 className="mt-4">Your Files</h3>
      <ul className="list-group mt-2">
        {files.length === 0 && <li className="list-group-item">(no files uploaded yet)</li>}
        {files.map(f => (
          <li className="list-group-item d-flex justify-content-between align-items-center" key={f}>
            <span>{f}</span>
            <div>
              <button className="btn btn-sm btn-secondary me-2" onClick={() => onDownload(f)}>Download</button>
              <button className="btn btn-sm btn-danger" onClick={() => onDelete(f)}>Delete</button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}
