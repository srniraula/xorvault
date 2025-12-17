import React, {useState} from 'react'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

export default function FileUpload({onSuccess, onClientAssigned}){
  const [file, setFile] = useState(null)
  const [filename, setFilename] = useState('')
  const [uploading, setUploading] = useState(false)
  const [msg, setMsg] = useState(null)

  const submit = async (e) => {
    e.preventDefault()
    if(!file){ setMsg({type:'danger', text:'No file selected'}); return }
    const fd = new FormData()
    fd.append('file', file)
    const localClient = localStorage.getItem('clientId')
    if(localClient){ fd.append('clientId', localClient) }
    if(filename) fd.append('filename', filename)

    setUploading(true); setMsg(null)
    try{
      const res = await fetch(`${API_BASE}/files`, { method: 'POST', body: fd })
      const data = await res.json()
      if(!res.ok) throw new Error(data.message || 'upload failed')
      setFile(null); setFilename('')
      setMsg({type:'success', text:'Uploaded'})
      // If server returned assigned client id, persist it
      if(data.clientId){
        localStorage.setItem('clientId', String(data.clientId))
        if(onClientAssigned) onClientAssigned(data.clientId)
      }
      if(onSuccess) onSuccess(data)
    }catch(err){ setMsg({type:'danger', text: err.message}) }
    setUploading(false)
  }

  return (
    <form className="card p-3" onSubmit={submit}>
      <div className="mb-2">
        <label className="form-label">File</label>
        <input type="file" className="form-control" onChange={e => setFile(e.target.files?.[0] || null)} />
      </div>
      <div className="mb-2">
        <label className="form-label">Filename (optional)</label>
        <input className="form-control" value={filename} onChange={e=>setFilename(e.target.value)} />
      </div>
      <button className="btn btn-primary" disabled={uploading}>{uploading ? 'Uploading...' : 'Upload'}</button>
      {msg && <div className={`mt-2 alert alert-${msg.type}`}>{msg.text}</div>}
    </form>
  )
}
