import React, { useState, useEffect } from 'react'
import FilesPage from './pages/Files'
import Auth from './components/Auth'

export default function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [authToken, setAuthToken] = useState(null)
  const [username, setUsername] = useState(null)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    const token = localStorage.getItem('authToken')
    const user = localStorage.getItem('username')
    if (token && user) {
      setAuthToken(token)
      setUsername(user)
      setIsAuthenticated(true)
    }
    setIsLoading(false)
  }, [])

  const handleAuthSuccess = (token, user) => {
    localStorage.setItem('authToken', token)
    localStorage.setItem('username', user)
    setAuthToken(token)
    setUsername(user)
    setIsAuthenticated(true)
  }

  const handleLogout = () => {
    localStorage.removeItem('authToken')
    localStorage.removeItem('username')
    setAuthToken(null)
    setUsername(null)
    setIsAuthenticated(false)
  }

  if (isLoading) {
    return (
      <div className="d-flex justify-content-center align-items-center" style={{ minHeight: '100vh' }}>
        <div className="spinner-border text-primary" role="status"></div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Auth onAuthSuccess={handleAuthSuccess} />
  }

  return (
    <div style={{ minHeight: '100vh', backgroundColor: '#f0f2f5' }}>
      {/* Top navbar */}
      <nav className="navbar navbar-dark shadow-sm" style={{ background: 'linear-gradient(135deg,#0d1b2a,#1b3a5c)' }}>
        <div className="container">
          <span className="navbar-brand fw-bold fs-5">
            <i className="bi bi-hdd-network-fill me-2"></i>XORFS
          </span>
          <div className="d-flex align-items-center gap-3">
            <span className="text-white-50 small d-flex align-items-center gap-1">
              <i className="bi bi-person-circle text-white"></i>
              <span className="text-white fw-medium">{username}</span>
            </span>
            <button className="btn btn-outline-light btn-sm" onClick={handleLogout}>
              <i className="bi bi-box-arrow-right me-1"></i>Logout
            </button>
          </div>
        </div>
      </nav>

      <div className="container py-4">
        <FilesPage authToken={authToken} username={username} />
      </div>
    </div>
  )
}
