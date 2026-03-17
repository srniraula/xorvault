# Temporary Files and Directories Used by Webserver

## Overview
This document lists all temporary directories and files used by the webserver during upload and download operations.

---

## 1. Upload Chunks (for chunked uploads >10MB)

Storage location: `/tmp/dfs_uploads/`

### View upload chunks:
```bash
ls -lh /tmp/dfs_uploads/
du -sh /tmp/dfs_uploads/
```

---

## 2. Download Temporary Files

Storage location: `/tmp/dfs_downloads/`

### View download files:
```bash
ls -lh /tmp/dfs_downloads/
du -sh /tmp/dfs_downloads/
```

---

## 3. Webserver Logs (user upload/download logs)

Storage location: `webserver_logs/`

### View webserver logs:
```bash
ls -lh webserver_logs/
du -sh webserver_logs/
```

---

## 4. Client Logs

Storage location: `client_logs/`

### View client logs:
```bash
ls -lh client_logs/
du -sh client_logs/
```

---

## 5. Log Files (chunkserver logs)

Storage location: `log_files/`

### View log files:
```bash
ls -lh log_files/
du -sh log_files/
```

---

## View All Temporary Files at Once

```bash
echo "=== Upload chunks ===" && ls -lh /tmp/dfs_uploads/ 2>/dev/null || echo "No upload chunks" && \
echo -e "\n=== Download files ===" && ls -lh /tmp/dfs_downloads/ 2>/dev/null || echo "No download files" && \
echo -e "\n=== Webserver logs ===" && ls -lh webserver_logs/ && \
echo -e "\n=== Client logs ===" && ls -lh client_logs/ && \
echo -e "\n=== Log files ===" && ls -lh log_files/
```

---

## Total Space Used by All Directories

```bash
echo "Upload chunks:" && du -sh /tmp/dfs_uploads/ 2>/dev/null || echo "0" && \
echo "Download files:" && du -sh /tmp/dfs_downloads/ 2>/dev/null || echo "0" && \
echo "Webserver logs:" && du -sh webserver_logs/ && \
echo "Client logs:" && du -sh client_logs/ && \
echo "Log files:" && du -sh log_files/
```

---

## Quick Summary of All Directories

```bash
tree -L 2 webserver_logs/ client_logs/ log_files/ 2>/dev/null || \
find webserver_logs client_logs log_files -type f | head -20
```

---

## Cleanup Operations

### Remove Old Upload Chunks (older than 24 hours)

```bash
find /tmp/dfs_uploads -type d -mtime +1 -exec rm -rf {} \;
```

### Remove Old Download Files (older than 24 hours)

```bash
find /tmp/dfs_downloads -type f -mtime +1 -delete 2>/dev/null
```

### Remove All Temporary Upload Files

```bash
rm -rf /tmp/dfs_uploads/*
```

### Remove All Temporary Download Files

```bash
rm -rf /tmp/dfs_downloads/
```

---

## Notes

- **Upload chunks**: Stored in `/tmp/dfs_uploads/` during chunked file uploads (>10MB)
- **Download files**: Stored in `/tmp/dfs_downloads/` during file downloads
- **Webserver logs**: Contain user-specific upload/download logs in `webserver_logs/`
- **Cleanup**: Upload chunks are automatically cleaned up 60 seconds after successful upload
- **Cleanup**: Download files are automatically cleaned up after download completion
