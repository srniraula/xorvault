// package main

// import (
// 	"bytes"
// 	"context"
// 	"io"
// 	"mime/multipart"
// 	"net/http"
// 	"net/http/httptest"
// 	"os"
// 	"testing"
// )

// // mockClient implements dfsclient.Client for tests
// type mockClient struct {
// 	UploadFunc   func(ctxCtx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, error)
// 	ListFunc     func(ctxCtx context.Context, clientID int64) ([]string, error)
// 	DownloadFunc func(ctxCtx context.Context, clientID int64, filename string, destPath string, username string) error
// 	DeleteFunc   func(ctxCtx context.Context, clientID int64, filename string, username string) (int, error)
// }

// func (m *mockClient) UploadFile(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, error) {
// 	if m.UploadFunc != nil {
// 		return m.UploadFunc(ctx, clientID, filename, data, size, username)
// 	}
// 	return 0, nil
// }
// func (m *mockClient) ListFiles(ctx context.Context, clientID int64) ([]string, error) {
// 	if m.ListFunc != nil {
// 		return m.ListFunc(ctx, clientID)
// 	}
// 	return nil, nil
// }
// func (m *mockClient) DownloadFile(ctx context.Context, clientID int64, filename string, destPath string, username string) error {
// 	if m.DownloadFunc != nil {
// 		return m.DownloadFunc(ctx, clientID, filename, destPath, username)
// 	}
// 	return nil
// }
// func (m *mockClient) DeleteFile(ctx context.Context, clientID int64, filename string, username string) (int, error) {
// 	if m.DeleteFunc != nil {
// 		return m.DeleteFunc(ctx, clientID, filename, username)
// 	}
// 	return 0, nil
// }

// func TestListHandler(t *testing.T) {
// 	mc := &mockClient{}
// 	mc.ListFunc = func(ctx context.Context, clientID int64) ([]string, error) { return []string{"a.txt", "b.txt"}, nil }

// 	r := NewRouter(mc)
// 	req := httptest.NewRequest("GET", "/files?clientId=1", nil)
// 	rec := httptest.NewRecorder()
// 	r.ServeHTTP(rec, req)

// 	if rec.Code != http.StatusOK {
// 		t.Fatalf("expected 200 got %d", rec.Code)
// 	}

// 	// Also accept lowercase query param
// 	req2 := httptest.NewRequest("GET", "/files?clientid=1", nil)
// 	rec2 := httptest.NewRecorder()
// 	r.ServeHTTP(rec2, req2)
// 	if rec2.Code != http.StatusOK {
// 		t.Fatalf("expected 200 got %d for clientid", rec2.Code)
// 	}
// }

// func TestUploadHandler(t *testing.T) {
// 	mc := &mockClient{}
// 	mc.UploadFunc = func(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, error) {
// 		return 123, nil
// 	}
// 	mc.ListFunc = func(ctx context.Context, clientID int64) ([]string, error) { return []string{"test.txt"}, nil }

// 	r := NewRouter(mc)

// 	// build multipart form
// 	body := &bytes.Buffer{}
// 	writer := multipart.NewWriter(body)
// 	_ = writer.WriteField("clientId", "1")
// 	part, _ := writer.CreateFormFile("file", "test.txt")
// 	part.Write([]byte("hello"))
// 	writer.Close()

// 	req := httptest.NewRequest("POST", "/files", body)
// 	req.Header.Set("Content-Type", writer.FormDataContentType())
// 	rec := httptest.NewRecorder()
// 	r.ServeHTTP(rec, req)

// 	if rec.Code != http.StatusCreated {
// 		t.Fatalf("expected 201 got %d", rec.Code)
// 	}
// }

// func TestDownloadHandler(t *testing.T) {
// 	mc := &mockClient{}
// 	mc.ListFunc = func(ctx context.Context, clientID int64) ([]string, error) { return []string{"foo.txt"}, nil }
// 	mc.DownloadFunc = func(ctx context.Context, clientID int64, filename string, destPath string, username string) error {
// 		// write a small file to destPath
// 		os.WriteFile(destPath, []byte("downloaded"), 0644)
// 		return nil
// 	}

// 	r := NewRouter(mc)
// 	req := httptest.NewRequest("GET", "/files/1/foo.txt", nil)
// 	rec := httptest.NewRecorder()
// 	r.ServeHTTP(rec, req)

// 	if rec.Code != http.StatusOK {
// 		t.Fatalf("expected 200 got %d", rec.Code)
// 	}
// 	if rec.Header().Get("Content-Disposition") == "" {
// 		t.Fatalf("missing Content-Disposition header")
// 	}
// }

// func TestDeleteHandler(t *testing.T) {
// 	mc := &mockClient{}
// 	mc.DeleteFunc = func(ctx context.Context, clientID int64, filename string, username string) (int, error) {
// 		return 3, nil
// 	}

// 	r := NewRouter(mc)
// 	req := httptest.NewRequest("DELETE", "/files/1/foo.txt", nil)
// 	rec := httptest.NewRecorder()
// 	r.ServeHTTP(rec, req)

// 	if rec.Code != http.StatusOK {
// 		t.Fatalf("expected 200 got %d", rec.Code)
// 	}
// }

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

	"dfs-project/pkg/dfsclient"
)

// mockClient implements dfsclient.Client for tests.
// UploadFile and DownloadFile now return TransferStats to match the updated interface.
type mockClient struct {
	UploadFunc   func(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, dfsclient.TransferStats, error)
	ListFunc     func(ctx context.Context, clientID int64) ([]string, error)
	DownloadFunc func(ctx context.Context, clientID int64, filename string, destPath string, username string) (dfsclient.TransferStats, error)
	DeleteFunc   func(ctx context.Context, clientID int64, filename string, username string) (int, error)
}

func (m *mockClient) UploadFile(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, dfsclient.TransferStats, error) {
	if m.UploadFunc != nil {
		return m.UploadFunc(ctx, clientID, filename, data, size, username)
	}
	return 0, dfsclient.TransferStats{}, nil
}

func (m *mockClient) ListFiles(ctx context.Context, clientID int64) ([]string, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, clientID)
	}
	return nil, nil
}

func (m *mockClient) DownloadFile(ctx context.Context, clientID int64, filename string, destPath string, username string) (dfsclient.TransferStats, error) {
	if m.DownloadFunc != nil {
		return m.DownloadFunc(ctx, clientID, filename, destPath, username)
	}
	return dfsclient.TransferStats{}, nil
}

func (m *mockClient) DeleteFile(ctx context.Context, clientID int64, filename string, username string) (int, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, clientID, filename, username)
	}
	return 0, nil
}

func TestListHandler(t *testing.T) {
	mc := &mockClient{}
	mc.ListFunc = func(ctx context.Context, clientID int64) ([]string, error) {
		return []string{"a.txt", "b.txt"}, nil
	}

	r := NewRouter(mc)
	req := httptest.NewRequest("GET", "/files?clientId=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}

	// Also accept lowercase query param
	req2 := httptest.NewRequest("GET", "/files?clientid=1", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d for clientid", rec2.Code)
	}
}

func TestUploadHandler(t *testing.T) {
	mc := &mockClient{}
	mc.UploadFunc = func(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, dfsclient.TransferStats, error) {
		return 123, dfsclient.TransferStats{StripeCount: 1, ChunksAttempted: 3, ChunksSucceeded: 3}, nil
	}
	mc.ListFunc = func(ctx context.Context, clientID int64) ([]string, error) {
		return []string{"test.txt"}, nil
	}

	r := NewRouter(mc)

	// build multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("clientId", "1")
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello"))
	writer.Close()

	req := httptest.NewRequest("POST", "/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d", rec.Code)
	}
}

func TestDownloadHandler(t *testing.T) {
	mc := &mockClient{}
	mc.ListFunc = func(ctx context.Context, clientID int64) ([]string, error) {
		return []string{"foo.txt"}, nil
	}
	mc.DownloadFunc = func(ctx context.Context, clientID int64, filename string, destPath string, username string) (dfsclient.TransferStats, error) {
		// write a small file to destPath so c.File() succeeds
		os.WriteFile(destPath, []byte("downloaded"), 0644)
		return dfsclient.TransferStats{StripeCount: 1, ChunksAttempted: 3, ChunksSucceeded: 3}, nil
	}

	r := NewRouter(mc)
	req := httptest.NewRequest("GET", "/files/1/foo.txt", nil)
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
	mc.DeleteFunc = func(ctx context.Context, clientID int64, filename string, username string) (int, error) {
		return 3, nil
	}

	r := NewRouter(mc)
	req := httptest.NewRequest("DELETE", "/files/1/foo.txt", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}
