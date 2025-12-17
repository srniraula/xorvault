import React, {useEffect, useState} from 'react'
import FileUpload from '../components/FileUpload'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

export default function FilesPage(){
  const [clientId, setClientId] = useState(() => {
    const cid = localStorage.getItem('clientId')
    return cid ? Number(cid) : null
  })
  const [files, setFiles] = useState([])
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState(null)

  const loadFiles = async () => {
    if(!clientId){ setMessage({type:'warning', text:'You are not registered yet. Upload a file to get a client id.'}); return }
    setLoading(true)
    setMessage(null)
    try{
      const res = await fetch(`${API_BASE}/files?clientId=${clientId}`)
      if(!res.ok) throw new Error(await res.text())
      const data = await res.json()
      setFiles(data.filenames || [])
    }catch(err){
      setMessage({type:'danger', text: err.message})
    }finally{ setLoading(false) }
  }

  useEffect(()=>{ if(clientId) loadFiles() }, [clientId])

  const onUploadSuccess = (data) => {
    setMessage({type:'success', text:'Upload succeeded'})
    // if server returned clientId, set it
    if(data && data.clientId){
      setClientId(Number(data.clientId))
    }
    loadFiles()
  }

  const onDelete = async (filename) => {
    if(!confirm(`Delete ${filename}?`)) return
    try{
      const res = await fetch(`${API_BASE}/files/${clientId}/${encodeURIComponent(filename)}`, { method: 'DELETE' })
      if(!res.ok) throw new Error(await res.text())
      setMessage({type: 'success', text: 'Deleted'})
      loadFiles()
    }catch(err){ setMessage({type:'danger', text: err.message}) }
  }

  const onDownload = (filename) => {
    window.location = `${API_BASE}/files/${clientId}/${encodeURIComponent(filename)}`
  }

  return (
    <div>
      <div className="mb-3 d-flex gap-2 align-items-center">
        {clientId ? (
          <>
            <div className="badge bg-primary">Client ID: {clientId}</div>
            <button className="btn btn-outline-primary" onClick={loadFiles} disabled={loading}>View files</button>
          </>
        ) : (
          <div className="text-muted">You are not registered yet. Upload a file to receive a client ID.</div>
        )}
      </div>

      <FileUpload onSuccess={onUploadSuccess} onClientAssigned={(id)=>{ setClientId(Number(id)); setMessage({type:'info', text: `Assigned client id ${id}`}) }} />

      {message && <div className={`alert alert-${message.type} mt-3`}>{message.text}</div>}

      <h3 className="mt-4">Files</h3>
      <ul className="list-group mt-2">
        {files.length === 0 && <li className="list-group-item">(no files)</li>}
        {files.map(f => (
          <li className="list-group-item d-flex justify-content-between align-items-center" key={f}>
            <span>{f}</span>
            <div>
              <button className="btn btn-sm btn-secondary me-2" onClick={()=>onDownload(f)}>Download</button>
              <button className="btn btn-sm btn-danger" onClick={()=>onDelete(f)}>Delete</button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}
