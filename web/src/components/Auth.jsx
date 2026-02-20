import React, { useState } from 'react';
import './Auth.css';

const API_BASE = import.meta.env.VITE_API_BASE || `${window.location.protocol}//${window.location.hostname}:8080`

const Auth = ({ onAuthSuccess }) => {
  const [isSignUp, setIsSignUp] = useState(false);
  const [formData, setFormData] = useState({
    userId: '',
    password: ''
  });
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsLoading(true);
    setError('');

    try {
      const endpoint = isSignUp ? '/auth/register' : '/auth/login';
      const body = {
        userId: formData.userId, 
        password: formData.password 
      };

      const response = await fetch(`${API_BASE}${endpoint}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(body),
      });

      const data = await response.json();

      if (data.success) {
        // Store the JWT token in localStorage
        localStorage.setItem('authToken', data.token);
        localStorage.setItem('userId', data.userId);
        
        // Call the success callback
        onAuthSuccess(data.token, data.userId);
      } else {
        setError(data.message || 'Authentication failed');
      }
    } catch (err) {
      setError('Network error. Please try again.');
      console.error('Auth error:', err);
    } finally {
      setIsLoading(false);
    }
  };

  const handleInputChange = (e) => {
    setFormData({
      ...formData,
      [e.target.name]: e.target.value
    });
  };

  return (
    <div className="auth-container">
      <div className="auth-card">
        <h2>{isSignUp ? 'Sign Up' : 'Login'}</h2>
        
        {error && <div className="error-message">{error}</div>}
        
        <form onSubmit={handleSubmit} className="auth-form">
          <div className="form-group">
            <label htmlFor="userId">{isSignUp ? 'Choose User ID:' : 'User ID:'}</label>
            <input
              type="text"
              id="userId"
              name="userId"
              value={formData.userId}
              onChange={handleInputChange}
              required
              placeholder={isSignUp ? "Choose your User ID (3-20 characters)" : "Enter your User ID"}
              maxLength={20}
              minLength={3}
            />
            {isSignUp && (
              <small className="userId-hint">
                3-20 characters, must start with a letter, letters/numbers/underscores only
              </small>
            )}
          </div>
          
          <div className="form-group">
            <label htmlFor="password">Password:</label>
            <input
              type="password"
              id="password"
              name="password"
              value={formData.password}
              onChange={handleInputChange}
              required
              placeholder="Enter your password"
              minLength={6}
            />
            <small className="password-hint">
              Password must be at least 6 characters with letters, numbers, or special characters
            </small>
          </div>
          
          <button 
            type="submit" 
            disabled={isLoading}
            className="auth-button"
          >
            {isLoading ? 'Processing...' : (isSignUp ? 'Sign Up' : 'Login')}
          </button>
        </form>
        
        <div className="auth-toggle">
          <p>
            {isSignUp ? 'Already have an account?' : "Don't have an account?"}
            <button 
              type="button" 
              onClick={() => {
                setIsSignUp(!isSignUp);
                setError('');
                setFormData({ userId: '', password: '' });
              }}
              className="toggle-button"
            >
              {isSignUp ? 'Login' : 'Sign Up'}
            </button>
          </p>
        </div>

        {isSignUp && (
          <div className="signup-info">
            <p><strong>Note:</strong> Choose a unique User ID that you'll use to log in from any browser or device. 
            Make sure to remember both your User ID and password!</p>
          </div>
        )}
      </div>
    </div>
  );
};

export default Auth;