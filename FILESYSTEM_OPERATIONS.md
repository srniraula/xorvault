# XorFS File System Operations Guide

This guide demonstrates all the new file system operations added to XorFS RAID-5 distributed file system.

## New Features

### 1. Folder Hierarchy Support
- Create and manage folder structures
- Organize files in directories
- Hierarchical namespace (e.g., `documents/reports/2024/`)

### 2. Enhanced File Operations
- **Move/Rename**: Relocate files between folders
- **Preview**: View file content without full download
- **Detailed Listing**: See file sizes, upload timestamps, and folder structure

### 3. Metadata Tracking
- Upload timestamps for all files
- File size information
- Folder structure metadata

## Command Reference

### Folder Operations

#### Create a Folder
```bash
# Create a single folder
make mkdir FOLDER=documents

# Create nested folders (automatically creates parents)
make mkdir FOLDER=documents/reports/2024

# Create multiple levels
make mkdir FOLDER=projects/xorfs/docs
```

#### Remove a Folder
```bash
# Remove an empty folder
make rmdir FOLDER=documents

# Note: Folder must be empty (no files or subfolders)
```

### File Operations

#### Move or Rename Files
```bash
# Move file to folder
make mv SRC=report.pdf DEST=documents/report.pdf

# Rename a file
make mv SRC=old_name.pdf DEST=new_name.pdf

# Move and rename
make mv SRC=file.pdf DEST=documents/reports/annual_report.pdf

# Move from folder to folder
make mv SRC=drafts/report.pdf DEST=final/report.pdf
```

#### Preview File Content (cat)
```bash
# View text file content
make cat FILE=readme.txt

# Preview any file (shows preview for text, info for binary)
make cat FILE=documents/notes.md

# Preview shows:
# - Full content for small text files (< 64KB)
# - Preview of large files with size indication
# - Hex dump for binary files
```

### Listing Operations

#### Simple List (existing)
```bash
# List all files (flat view)
make ls
```

#### Detailed List (new)
```bash
# List all files and folders with details
make ls-detailed

# Output shows:
# - Type (FILE or DIR)
# - Path
# - Size (formatted: B, KB, MB, GB)
# - Upload timestamp

# List specific folder contents
make ls-detailed FOLDER=documents

# List subfolder
make ls-detailed FOLDER=documents/reports
```

### Upload with Folders
```bash
# You can now upload files with folder paths
# The file will be created at the specified path
# (Note: folder must exist first, or modify upload to auto-create)

# Example workflow:
make mkdir FOLDER=documents
make upload FILE=report.pdf  # uploads as report.pdf
make mv SRC=report.pdf DEST=documents/report.pdf

# Or organize after upload
make upload FILE=photo1.jpg
make mkdir FOLDER=photos/vacation
make mv SRC=photo1.jpg DEST=photos/vacation/photo1.jpg
```

## Example Workflows

### Workflow 1: Organizing Documents
```bash
# 1. Upload some files
make upload FILE=report1.pdf
make upload FILE=report2.pdf
make upload FILE=notes.txt

# 2. Create folder structure
make mkdir FOLDER=documents
make mkdir FOLDER=documents/reports
make mkdir FOLDER=documents/notes

# 3. Organize files
make mv SRC=report1.pdf DEST=documents/reports/report1.pdf
make mv SRC=report2.pdf DEST=documents/reports/report2.pdf
make mv SRC=notes.txt DEST=documents/notes/notes.txt

# 4. View organized structure
make ls-detailed
make ls-detailed FOLDER=documents
make ls-detailed FOLDER=documents/reports
```

### Workflow 2: Working with Text Files
```bash
# 1. Upload a text file
make upload FILE=readme.txt

# 2. Preview its content
make cat FILE=readme.txt

# 3. Move to documentation folder
make mkdir FOLDER=docs
make mv SRC=readme.txt DEST=docs/readme.txt

# 4. View folder
make ls-detailed FOLDER=docs
```

### Workflow 3: Multi-Client Organization
```bash
# Client 1: Create project structure
make mkdir FOLDER=project_alpha
make mkdir FOLDER=project_alpha/code
make mkdir FOLDER=project_alpha/docs
make upload FILE=main.go
make mv SRC=main.go DEST=project_alpha/code/main.go

# Client 2: Their own isolated structure
make mkdir FOLDER=personal
make mkdir FOLDER=personal/photos
make upload FILE=vacation.jpg
make mv SRC=vacation.jpg DEST=personal/photos/vacation.jpg

# Each client has their own isolated namespace
```

### Workflow 4: File Management
```bash
# Check what you have
make ls-detailed

# Preview a document
make cat FILE=document.txt

# Reorganize
make mkdir FOLDER=archive
make mv SRC=old_document.txt DEST=archive/old_document.txt

# Clean up empty folders
make rmdir FOLDER=old_folder

# Download something you need
make download FILE=important.pdf
```

## Important Notes

### Folder Paths
- Folder paths are relative to client root
- Use forward slashes `/` for path separation
- No leading or trailing slashes needed
- Example: `documents/reports/2024` (not `/documents/` or `documents/`)

### Client Isolation
- Each client has their own isolated file system
- Folders and files are per-client
- Client ID is stored in `.client_id` file

### Folder Operations
- **mkdir**: Creates folder and all parent folders automatically
- **rmdir**: Only works on empty folders (must delete files first)
- **mv**: Can move files between folders (destination folder must exist)

### File Listing
- `ls`: Simple list of filenames (backward compatible)
- `ls-detailed`: Shows type, size, timestamp, and path
- Both commands show only items in the specified folder (not recursive)

### Limits
- cat command previews first 64KB of file
- For full file content, use `download` command
- Folder depth is unlimited
- No restriction on folder/file name length (reasonable limits apply)

## Error Handling

### Common Errors and Solutions

**"folder already exists"**
- You're trying to create a folder that exists
- Solution: Use ls-detailed to check existing folders

**"folder not found"**  
- Destination folder doesn't exist for mv command
- Solution: Create folder with mkdir first

**"folder is not empty"**
- Trying to delete folder containing files or subfolders
- Solution: Delete contents first, or use delete for files

**"file not found"**
- Specified file doesn't exist
- Solution: Use ls or ls-detailed to check available files

**"access denied"**
- Trying to access another client's files
- Solution: Each client can only access their own files

## Performance Considerations

- **mkdir**: Instant (metadata only)
- **rmdir**: Instant (metadata only)
- **mv**: Instant (metadata only, no data movement)
- **ls-detailed**: Fast (metadata scan)
- **cat**: Depends on file size (max 64KB transferred)

All folder operations are metadata-only and don't move actual chunk data!

## Future Enhancements

Potential additions to the file system:
- Recursive folder listing
- Folder-level operations (delete folder and contents)
- Search functionality
- File metadata (tags, descriptions)
- Shared folders between clients
- Access control lists (ACLs)
