import React, { useState } from 'react';
import './Auth.css';

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

const Auth = ({ onAuthSuccess }) => {
  const [isSignUp, setIsSignUp] = useState(false);
  const [formData, setFormData] = useState({ username: '', password: '' });
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsLoading(true);
    setError('');
    try {
      const endpoint = isSignUp ? '/auth/register' : '/auth/login';
      const response = await fetch(`${API_BASE}${endpoint}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: formData.username, password: formData.password }),
      });
      const data = await response.json();
      if (data.success) {
        localStorage.setItem('authToken', data.token);
        localStorage.setItem('username', data.username);
        onAuthSuccess(data.token, data.username);
      } else {
        setError(data.message || 'Authentication failed');
      }
    } catch (err) {
      setError('Network error. Please try again.');
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="auth-container">
      <div className="auth-card">
        {/* Brand header */}
        <div className="text-center mb-4">
          <div style={{ fontSize: '2.8rem', color: '#1b3a5c' }}>
            <i className="bi bi-hdd-network-fill"></i>
          </div>
          <h2 className="mt-1 mb-0 fw-bold" style={{ color: '#0d1b2a' }}>XORFS</h2>
          <p className="text-muted small mb-0">Distributed File System</p>
        </div>

        {/* Tab toggle */}
        <div className="d-flex mb-4 rounded-2 overflow-hidden border">
          <button type="button"
            className={`btn flex-fill rounded-0 ${!isSignUp ? 'btn-primary' : 'btn-light'}`}
            onClick={() => { setIsSignUp(false); setError(''); }}>
            Sign In
          </button>
          <button type="button"
            className={`btn flex-fill rounded-0 ${isSignUp ? 'btn-primary' : 'btn-light'}`}
            onClick={() => { setIsSignUp(true); setError(''); }}>
            Sign Up
          </button>
        </div>

        {error && <div className="alert alert-danger py-2 small mb-3" role="alert">{error}</div>}

        <form onSubmit={handleSubmit} className="auth-form">
          <div className="form-group">
            <label htmlFor="username">Username</label>
            <input
              type="text"
              id="username"
              name="username"
              value={formData.username}
              onChange={e => setFormData({ ...formData, username: e.target.value })}
              required
              placeholder={isSignUp ? 'Choose a username (3–20 chars)' : 'Enter your username'}
              maxLength={20}
              minLength={3}
              autoComplete="username"
            />
            {isSignUp && (
              <small className="userId-hint">
                3–20 characters, must start with a letter, letters/numbers/underscores only
              </small>
            )}
          </div>

          <div className="form-group">
            <label htmlFor="password">Password</label>
            <input
              type="password"
              id="password"
              name="password"
              value={formData.password}
              onChange={e => setFormData({ ...formData, password: e.target.value })}
              required
              placeholder="Enter your password"
              minLength={6}
              autoComplete={isSignUp ? 'new-password' : 'current-password'}
            />
            {isSignUp && (
              <small className="password-hint">
                Min 6 chars — must include a letter, a number, and a special character
              </small>
            )}
          </div>

          <button type="submit" className="auth-button" disabled={isLoading}>
            {isLoading ? 'Please wait…' : (isSignUp ? 'Create account' : 'Sign in')}
          </button>
        </form>
      </div>
    </div>
  );
};

export default Auth;