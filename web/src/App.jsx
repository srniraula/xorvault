import React, { useState } from 'react'
import FilesPage from './pages/Files'
import LoginPage from './pages/Login'

export default function App() {
  const [user, setUser] = useState(() => localStorage.getItem('username'));
  const [password, setPassword] = useState(() => localStorage.getItem('password'));

  const handleLogin = (u, p) => {
    setUser(u);
    setPassword(p);
  };

  const handleLogout = () => {
    localStorage.removeItem('username');
    localStorage.removeItem('password');
    setUser(null);
    setPassword(null);
  };

  return (
    <div className="container py-5">
      <header className="d-flex justify-content-between align-items-center mb-5">
        <div>
          <h1 className="fw-bold text-primary mb-0">XORFS <span className="text-dark">Cloud</span></h1>
          <p className="text-muted small">Secure Distributed Storage</p>
        </div>
        {user && (
          <div className="d-flex align-items-center gap-3">
            <span className="text-muted small d-none d-sm-block">Logged in as <strong className="text-dark">{user}</strong></span>
            <button className="btn btn-outline-danger btn-sm rounded-pill px-3" onClick={handleLogout}>
              <i className="bi bi-box-arrow-right me-1"></i> Logout
            </button>
          </div>
        )}
      </header>

      {!user ? (
        <LoginPage onLogin={handleLogin} />
      ) : (
        <main className="animate__animated animate__fadeIn">
          <FilesPage username={user} password={password} />
        </main>
      )}
    </div>
  )
}
