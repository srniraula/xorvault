import React, { useState } from 'react';

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`;

export default function LoginPage({ onLogin }) {
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [isRegister, setIsRegister] = useState(false);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');

    const handleSubmit = async (e) => {
        e.preventDefault();
        setError('');
        if (!username.trim()) { setError('Username is required'); return; }
        if (password.length !== 6 || !/^\d+$/.test(password)) {
            setError('Password must be exactly 6 digits');
            return;
        }

        setLoading(true);
        try {
            const res = await fetch(`${API_BASE}/auth`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password, is_register: isRegister })
            });
            const data = await res.json();
            if (!res.ok || !data.success) {
                throw new Error(data.message || 'Authentication failed');
            }

            localStorage.setItem('username', username);
            localStorage.setItem('password', password);
            onLogin(username, password);
        } catch (err) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="row justify-content-center mt-5">
            <div className="col-md-5 col-lg-4">
                <div className="card shadow-lg border-0 rounded-4">
                    <div className="card-body p-5">
                        <div className="text-center mb-4">
                            <div className="bg-primary bg-opacity-10 d-inline-block p-3 rounded-circle mb-3">
                                <i className="bi bi-shield-lock-fill text-primary" style={{ fontSize: '2.5rem' }}></i>
                            </div>
                            <h2 className="fw-bold mt-2">{isRegister ? 'Create Account' : 'Welcome Back'}</h2>
                            <p className="text-muted small">{isRegister ? 'Register to start using XORFS' : 'Sign in to access your secure storage'}</p>
                        </div>

                        <form onSubmit={handleSubmit}>
                            <div className="form-floating mb-3">
                                <input
                                    type="text"
                                    className="form-control rounded-3 border-light shadow-sm"
                                    id="usernameInput"
                                    placeholder="Username"
                                    value={username}
                                    onChange={(e) => setUsername(e.target.value)}
                                    required
                                />
                                <label htmlFor="usernameInput">Username</label>
                            </div>

                            <div className="form-floating mb-3">
                                <input
                                    type="password"
                                    className="form-control rounded-3 border-light shadow-sm"
                                    id="passwordInput"
                                    placeholder="6-digit Password"
                                    value={password}
                                    maxLength={6}
                                    onChange={(e) => setPassword(e.target.value.replace(/\D/g, ''))}
                                    required
                                />
                                <label htmlFor="passwordInput">6-digit PIN</label>
                            </div>

                            {error && <div className="alert alert-danger py-2 rounded-3 small animate__animated animate__shakeX">{error}</div>}

                            <button type="submit" disabled={loading} className="btn btn-primary w-100 py-3 rounded-3 fw-bold mb-3 shadow-sm transition-all mt-2">
                                {loading ? <span className="spinner-border spinner-border-sm me-2"></span> : null}
                                {isRegister ? 'Register Account' : 'Login Securely'}
                            </button>

                            <div className="text-center mt-3">
                                <span className="text-muted small">
                                    {isRegister ? 'Already have an account?' : "Don't have an account?"}{' '}
                                    <button
                                        type="button"
                                        className="btn btn-link p-0 small text-decoration-none fw-bold"
                                        onClick={() => { setIsRegister(!isRegister); setError(''); }}
                                    >
                                        {isRegister ? 'Login' : 'Register Now'}
                                    </button>
                                </span>
                            </div>
                        </form>
                    </div>
                </div>

                <div className="mt-5 text-center text-muted small opacity-50">
                    <p className="mb-0">&copy; 2026 XORFS Cloud Storage</p>
                    <p>End-to-end distributed encryption</p>
                </div>
            </div>
        </div>
    );
}
