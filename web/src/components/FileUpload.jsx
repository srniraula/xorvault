import React, {useState} from 'react'
import ChunkedUploader from '../utils/ChunkedUploader'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

// Large file threshold - files above this will use chunked upload
const CHUNKED_UPLOAD_THRESHOLD = 10 * 1024 * 1024 // 10MB

export default function FileUpload({onSuccess, onClientAssigned}){
  const [file, setFile] = useState(null)
  const [filename, setFilename] = useState('')
  const [uploading, setUploading] = useState(false)
  const [progress, setProgress] = useState(0)
  const [uploadMethod, setUploadMethod] = useState('')
  const [msg, setMsg] = useState(null)

  const formatFileSize = (bytes) => {
    if (bytes === 0) return '0 Bytes'
    const k = 1024
    const sizes = ['Bytes', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  }

  const regularUpload = async (formData) => {
    const res = await fetch(`${API_BASE}/files`, { method: 'POST', body: formData })
    const data = await res.json()
    if (!res.ok) throw new Error(data.message || 'upload failed')
    return data
  }

  const chunkedUpload = async () => {
    const localClient = localStorage.getItem('clientId')
    const uploader = new ChunkedUploader(file, API_BASE)
    
    uploader.onProgress = (percent) => {
      setProgress(percent)
    }
    
    uploader.onError = (error) => {
      console.error('Chunked upload error:', error)
    }
    
    const result = await uploader.upload(localClient, filename)
    return result
  }

  const submit = async (e) => {
    e.preventDefault()
    if(!file){ 
      setMsg({type:'danger', text:'No file selected'}) 
      return 
    }
    
    const isLargeFile = file.size > CHUNKED_UPLOAD_THRESHOLD
    setUploadMethod(isLargeFile ? 'Chunked' : 'Standard')
    
    setUploading(true)
    setMsg(null)
    setProgress(0)
    
    try {
      let data
      
      if (isLargeFile) {
        // Use chunked upload for large files
        data = await chunkedUpload()
      } else {
        // Use regular upload for small files
        const fd = new FormData()
        fd.append('file', file)
        const localClient = localStorage.getItem('clientId')
        if(localClient) fd.append('clientId', localClient)
        if(filename) fd.append('filename', filename)
        
        data = await regularUpload(fd)
        setProgress(100)
      }
      
      // Reset form
      setFile(null)
      setFilename('')
      setProgress(0)
      setUploadMethod('')
      
      setMsg({
        type:'success', 
        text: `Uploaded successfully! ${isLargeFile ? '(Chunked)' : '(Standard)'}`
      })
      
      // Handle client ID assignment
      if(data.clientId){
        localStorage.setItem('clientId', String(data.clientId))
        if(onClientAssigned) onClientAssigned(data.clientId)
      }
      
      if(onSuccess) onSuccess(data)
      
    } catch(err) { 
      setProgress(0)
      setUploadMethod('')
      setMsg({type:'danger', text: err.message}) 
    }
    
    setUploading(false)
  }

  return (
    <form className="card p-3" onSubmit={submit}>
      <div className="mb-2">
        <label className="form-label">File</label>
        <input 
          type="file" 
          className="form-control" 
          onChange={e => setFile(e.target.files?.[0] || null)} 
        />
        {file && (
          <div className="mt-1">
            <small className="text-muted">
              Size: {formatFileSize(file.size)}
              {file.size > CHUNKED_UPLOAD_THRESHOLD && (
                <span className="badge bg-info ms-2">Will use chunked upload</span>
              )}
            </small>
          </div>
        )}
      </div>
      
      <div className="mb-2">
        <label className="form-label">Filename (optional)</label>
        <input 
          className="form-control" 
          value={filename} 
          onChange={e=>setFilename(e.target.value)} 
        />
      </div>
      
      {uploading && progress > 0 && (
        <div className="mb-3">
          <div className="d-flex justify-content-between mb-1">
            <small>Upload Progress ({uploadMethod})</small>
            <small>{progress}%</small>
          </div>
          <div className="progress">
            <div 
              className="progress-bar" 
              role="progressbar" 
              style={{width: `${progress}%`}}
              aria-valuenow={progress} 
              aria-valuemin="0" 
              aria-valuemax="100"
            ></div>
          </div>
        </div>
      )}
      
      <button className="btn btn-primary" disabled={uploading}>
        {uploading ? `Uploading... ${progress}%` : 'Upload'}
      </button>
      
      {msg && <div className={`mt-2 alert alert-${msg.type}`}>{msg.text}</div>}
    </form>
  )
}
