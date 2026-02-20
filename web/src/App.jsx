import React, { useState, useEffect } from 'react'
import FilesPage from './pages/Files'
import Auth from './components/Auth'

export default function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [authToken, setAuthToken] = useState(null);
  const [userId, setUserId] = useState(null);
  const [isLoading, setIsLoading] = useState(true);

  // Check for existing auth token on app load
  useEffect(() => {
    const storedToken = localStorage.getItem('authToken');
    const storedUserId = localStorage.getItem('userId');
    
    if (storedToken && storedUserId) {
      setAuthToken(storedToken);
      setUserId(storedUserId);
      setIsAuthenticated(true);
    }
    
    setIsLoading(false);
  }, []);

  const handleAuthSuccess = (token, userId) => {
    setAuthToken(token);
    setUserId(userId);
    setIsAuthenticated(true);
  };

  const handleLogout = () => {
    localStorage.removeItem('authToken');
    localStorage.removeItem('userId');
    setAuthToken(null);
    setUserId(null);
    setIsAuthenticated(false);
  };

  // Show loading spinner while checking authentication
  if (isLoading) {
    return (
      <div className="container py-4">
        <div className="text-center">
          <div className="spinner-border" role="status">
            <span className="sr-only">Loading...</span>
          </div>
        </div>
      </div>
    );
  }

  // Show auth form if not authenticated
  if (!isAuthenticated) {
    return <Auth onAuthSuccess={handleAuthSuccess} />;
  }

  // Show main app if authenticated
  return (
    <div className="container py-4">
      <div className="d-flex justify-content-between align-items-center mb-4">
        <h1 className="mb-0">XORFS Web UI</h1>
        <div className="user-info">
          <span className="me-3">User ID: {userId}</span>
          <button 
            className="btn btn-outline-secondary btn-sm"
            onClick={handleLogout}
          >
            Logout
          </button>
        </div>
      </div>
      <FilesPage authToken={authToken} userId={userId} />
    </div>
  );
}
