package main

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// mockClient implements dfsclient.Client for tests
type mockClient struct {
	AuthFunc     func(ctx context.Context, username, password string, isRegister bool) (bool, string, error)
	UploadFunc   func(ctx context.Context, username, password string, filename string, data io.Reader, size int64) (string, error)
	ListFunc     func(ctx context.Context, username, password string) ([]string, error)
	DownloadFunc func(ctx context.Context, username, password string, filename string, destPath string) error
	DeleteFunc   func(ctx context.Context, username, password string, filename string) (int, error)
}

func (m *mockClient) Authenticate(ctx context.Context, username, password string, isRegister bool) (bool, string, error) {
	if m.AuthFunc != nil {
		return m.AuthFunc(ctx, username, password, isRegister)
	}
	return true, "ok", nil
}
func (m *mockClient) UploadFile(ctx context.Context, username, password string, filename string, data io.Reader, size int64) (string, error) {
	if m.UploadFunc != nil {
		return m.UploadFunc(ctx, username, password, filename, data, size)
	}
	return "", nil
}
func (m *mockClient) ListFiles(ctx context.Context, username, password string) ([]string, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, username, password)
	}
	return nil, nil
}
func (m *mockClient) DownloadFile(ctx context.Context, username, password string, filename string, destPath string) error {
	if m.DownloadFunc != nil {
		return m.DownloadFunc(ctx, username, password, filename, destPath)
	}
	return nil
}
func (m *mockClient) DeleteFile(ctx context.Context, username, password string, filename string) (int, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, username, password, filename)
	}
	return 0, nil
}

func TestListHandler(t *testing.T) {
	mc := &mockClient{}
	mc.ListFunc = func(ctx context.Context, username, password string) ([]string, error) {
		return []string{"a.txt", "b.txt"}, nil
	}

	r := NewRouter(mc)
	req := httptest.NewRequest("GET", "/files?username=user1", nil)
	req.Header.Set("X-DFS-Password", "123456")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}

	// Also accept 'user' query param
	req2 := httptest.NewRequest("GET", "/files?user=user1", nil)
	req2.Header.Set("X-DFS-Password", "123456")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d for user", rec2.Code)
	}
}

func TestUploadHandler(t *testing.T) {
	mc := &mockClient{}
	mc.UploadFunc = func(ctx context.Context, username, password string, filename string, data io.Reader, size int64) (string, error) {
		return "user1", nil
	}
	mc.ListFunc = func(ctx context.Context, username, password string) ([]string, error) {
		return []string{"test.txt"}, nil
	}

	r := NewRouter(mc)

	// build multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("username", "user1")
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello"))
	writer.Close()

	req := httptest.NewRequest("POST", "/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-DFS-Password", "123456")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d", rec.Code)
	}
}

func TestDownloadHandler(t *testing.T) {
	mc := &mockClient{}
	mc.ListFunc = func(ctx context.Context, username, password string) ([]string, error) {
		return []string{"foo.txt"}, nil
	}
	mc.DownloadFunc = func(ctx context.Context, username, password string, filename string, destPath string) error {
		// write a small file to destPath
		os.WriteFile(destPath, []byte("downloaded"), 0644)
		return nil
	}

	r := NewRouter(mc)
	req := httptest.NewRequest("GET", "/files/user1/foo.txt", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	if rec.Header().Get("Content-Disposition") == "" {
		t.Fatalf("missing Content-Disposition header")
	}
}

func TestDeleteHandler(t *testing.T) {
	mc := &mockClient{}
	mc.DeleteFunc = func(ctx context.Context, username, password string, filename string) (int, error) { return 3, nil }

	r := NewRouter(mc)
	req := httptest.NewRequest("DELETE", "/files/user1/foo.txt", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}
