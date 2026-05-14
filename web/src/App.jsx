import React, { useState, useEffect } from 'react'
import FilesPage from './pages/Files'
import MetricsDashboard from './pages/MetricsDashboard'
import Auth from './components/Auth'

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`
const ADMIN_USERNAME = 'admin'
const ADMIN_PASSWORD = 'YourNewPassword123!'
const ADMIN_AUTH_KEY = 'isAdminAuthenticated'

function AdminLogin({ onAdminAuthSuccess }) {
  const [formData, setFormData] = useState({ username: '', password: '' })
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')

  const handleSubmit = async (e) => {
    e.preventDefault()
    setError('')

    const submittedUsername = formData.username.trim()
    const submittedPassword = formData.password.trim()

    if (submittedUsername !== ADMIN_USERNAME || submittedPassword !== ADMIN_PASSWORD) {
      setError('Invalid credentials. Only configured admin credentials are allowed.')
      return
    }

    setIsLoading(true)
    try {
      const response = await fetch(`${API_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: submittedUsername,
          password: submittedPassword,
        }),
      })
      const data = await response.json()
      if (!data.success) {
        throw new Error(data.message || 'Admin backend authentication failed')
      }

      onAdminAuthSuccess(data.token, data.username)
    } catch (err) {
      setError(err.message || 'Network error. Please try again.')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className="d-flex justify-content-center align-items-center" style={{ minHeight: '100vh', backgroundColor: '#f5f5f5' }}>
      <div className="card shadow-sm border-0" style={{ width: '100%', maxWidth: 420 }}>
        <div className="card-body p-4">
          <div className="text-center mb-4">
            <div style={{ fontSize: '2.4rem', color: '#0d1b2a' }}>
              <i className="bi bi-shield-lock-fill"></i>
            </div>
            <h4 className="fw-bold mb-1" style={{ color: '#0d1b2a' }}>Admin Access</h4>
            <p className="text-muted small mb-0">Sign in to view system metrics</p>
          </div>

          {error && <div className="alert alert-danger py-2 small" role="alert">{error}</div>}

          <form onSubmit={handleSubmit}>
            <div className="mb-3">
              <label htmlFor="admin-username" className="form-label">Username</label>
              <input
                id="admin-username"
                type="text"
                className="form-control"
                value={formData.username}
                onChange={(e) => setFormData({ ...formData, username: e.target.value })}
                autoComplete="username"
                required
              />
            </div>

            <div className="mb-3">
              <label htmlFor="admin-password" className="form-label">Password</label>
              <input
                id="admin-password"
                type="password"
                className="form-control"
                value={formData.password}
                onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                autoComplete="current-password"
                required
              />
            </div>

            <button type="submit" className="btn btn-primary w-100" disabled={isLoading}>
              {isLoading ? 'Authenticating...' : 'Login as Admin'}
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}

export default function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [authToken, setAuthToken] = useState(null)
  const [username, setUsername] = useState(null)
  const [isLoading, setIsLoading] = useState(true)
  const [isAdminAuthenticated, setIsAdminAuthenticated] = useState(false)
  const [pathName, setPathName] = useState(window.location.pathname)

  useEffect(() => {
    const token = localStorage.getItem('authToken')
    const user = localStorage.getItem('username')
    const adminAuth = localStorage.getItem(ADMIN_AUTH_KEY) === 'true'

    if (token && user) {
      setAuthToken(token)
      setUsername(user)
      setIsAuthenticated(true)
    }
    setIsAdminAuthenticated(adminAuth)
    setIsLoading(false)
  }, [])

  useEffect(() => {
    const onPopState = () => setPathName(window.location.pathname)
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const navigateTo = (targetPath) => {
    if (window.location.pathname === targetPath) return
    window.history.pushState({}, '', targetPath)
    setPathName(targetPath)
  }

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
    if (pathName === '/admin') {
      localStorage.removeItem(ADMIN_AUTH_KEY)
      setIsAdminAuthenticated(false)
    }
  }

  const handleAdminAuthSuccess = (token, user) => {
    handleAuthSuccess(token, user)
    localStorage.setItem(ADMIN_AUTH_KEY, 'true')
    setIsAdminAuthenticated(true)
  }

  const handleAdminLogout = () => {
    localStorage.removeItem(ADMIN_AUTH_KEY)
    setIsAdminAuthenticated(false)
  }

  if (isLoading) {
    return (
      <div className="d-flex justify-content-center align-items-center" style={{ minHeight: '100vh' }}>
        <div className="spinner-border text-primary" role="status"></div>
      </div>
    )
  }

  if (pathName === '/admin') {
    if (!isAdminAuthenticated) {
      return <AdminLogin onAdminAuthSuccess={handleAdminAuthSuccess} />
    }

    return (
      <div style={{ minHeight: '100vh', backgroundColor: '#f0f2f5' }}>
        <nav className="navbar navbar-dark shadow-sm" style={{ background: 'linear-gradient(135deg,#0d1b2a,#1b3a5c)' }}>
          <div className="container">
            <span className="navbar-brand fw-bold fs-5">
              <i className="bi bi-hdd-network-fill me-2"></i>XORFS Admin
            </span>
            <div className="d-flex align-items-center gap-2">
              <button className="btn btn-outline-light btn-sm" onClick={() => navigateTo('/')}>
                <i className="bi bi-house-door me-1"></i>Home
              </button>
              <button className="btn btn-outline-light btn-sm" onClick={handleAdminLogout}>
                <i className="bi bi-box-arrow-right me-1"></i>Admin Logout
              </button>
            </div>
          </div>
        </nav>
        <div className="container py-4">
          {isAuthenticated
            ? <MetricsDashboard authToken={authToken} />
            : <div className="alert alert-warning mb-0">Admin authenticated, but backend token missing. Log in again with admin credentials.</div>}
        </div>
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
            <button className="btn btn-outline-light btn-sm" onClick={() => navigateTo('/admin')}>
              <i className="bi bi-shield-lock me-1"></i>Admin
            </button>
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
